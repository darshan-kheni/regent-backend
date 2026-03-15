package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// --- Microsoft Graph response types ---

// MSEvent represents a single event from the Microsoft Graph API.
type MSEvent struct {
	ID               string        `json:"id"`
	Subject          string        `json:"subject"`
	BodyPreview      string        `json:"bodyPreview"`
	Start            MSDateTime    `json:"start"`
	End              MSDateTime    `json:"end"`
	Location         MSLocation    `json:"location"`
	IsAllDay         bool          `json:"isAllDay"`
	ShowAs           string        `json:"showAs"`
	Attendees        []MSAttendee  `json:"attendees"`
	Organizer        MSOrganizer   `json:"organizer"`
	IsOnlineMeeting  bool          `json:"isOnlineMeeting"`
	OnlineMeetingUrl string        `json:"onlineMeetingUrl"`
	Recurrence       *MSRecurrence `json:"recurrence"`
	Removed          *MSRemoved    `json:"@removed"`
}

// MSDateTime represents a Microsoft Graph dateTime/timeZone pair.
type MSDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// MSLocation represents the location of a Microsoft Graph event.
type MSLocation struct {
	DisplayName string `json:"displayName"`
}

// MSAttendee represents an attendee on a Microsoft Graph event.
type MSAttendee struct {
	EmailAddress MSEmailAddress `json:"emailAddress"`
	Type         string         `json:"type"`
	Status       MSStatus       `json:"status"`
}

// MSEmailAddress holds name and address for a Microsoft Graph email contact.
type MSEmailAddress struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// MSStatus holds the response status for a Microsoft Graph attendee.
type MSStatus struct {
	Response string `json:"response"`
}

// MSOrganizer represents the organizer of a Microsoft Graph event.
type MSOrganizer struct {
	EmailAddress MSEmailAddress `json:"emailAddress"`
}

// MSRecurrence represents recurrence information on a Microsoft Graph event.
type MSRecurrence struct {
	Pattern MSRecurrencePattern `json:"pattern"`
}

// MSRecurrencePattern describes the recurrence pattern type and interval.
type MSRecurrencePattern struct {
	Type     string `json:"type"`
	Interval int    `json:"interval"`
}

// MSRemoved is present on delta responses when an event has been deleted.
type MSRemoved struct {
	Reason string `json:"reason"`
}

// MSCalendarViewResponse is the top-level response from Microsoft Graph calendar endpoints.
type MSCalendarViewResponse struct {
	Value     []MSEvent `json:"value"`
	NextLink  string    `json:"@odata.nextLink"`
	DeltaLink string    `json:"@odata.deltaLink"`
}

// MSScheduleResponse is the response from the getSchedule endpoint.
type MSScheduleResponse struct {
	Value []MSScheduleItem `json:"value"`
}

// MSScheduleItem represents one user's schedule in a getSchedule response.
type MSScheduleItem struct {
	ScheduleID       string           `json:"scheduleId"`
	AvailabilityView string           `json:"availabilityView"`
	ScheduleItems    []MSScheduleSlot `json:"scheduleItems"`
}

// MSScheduleSlot is a single busy/tentative slot in a schedule response.
type MSScheduleSlot struct {
	Status string     `json:"status"`
	Start  MSDateTime `json:"start"`
	End    MSDateTime `json:"end"`
}

// --- Microsoft Graph REST client ---

// MicrosoftCalendarClient is a thin HTTP wrapper around the Microsoft Graph Calendar API.
type MicrosoftCalendarClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewMicrosoftCalendarClient creates a new Microsoft Graph calendar client.
// The provided httpClient should carry OAuth2 credentials (e.g. via oauth2.NewClient).
func NewMicrosoftCalendarClient(httpClient *http.Client) *MicrosoftCalendarClient {
	return &MicrosoftCalendarClient{
		httpClient: httpClient,
		baseURL:    "https://graph.microsoft.com/v1.0",
	}
}

// doRequest executes an HTTP request with JSON content negotiation.
// If body is non-nil it is marshaled to JSON. Returns the response; caller must close Body.
func (c *MicrosoftCalendarClient) doRequest(ctx context.Context, method, url string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &msGraphError{
			StatusCode: resp.StatusCode,
			Body:       string(errBody),
			header:     resp.Header,
		}
	}

	return resp, nil
}

// msGraphError wraps a non-2xx Microsoft Graph API response.
type msGraphError struct {
	StatusCode int
	Body       string
	header     http.Header
}

func (e *msGraphError) Error() string {
	return fmt.Sprintf("microsoft graph API error %d: %s", e.StatusCode, e.Body)
}

// Header implements the httpError interface used by extractRetryAfter.
func (e *msGraphError) Header() http.Header {
	return e.header
}

// CalendarView fetches events from the user's primary calendar within the given time range.
// Handles @odata.nextLink pagination automatically.
func (c *MicrosoftCalendarClient) CalendarView(ctx context.Context, start, end time.Time) ([]MSEvent, error) {
	url := fmt.Sprintf("%s/me/calendarView?startDateTime=%s&endDateTime=%s&$top=250",
		c.baseURL,
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339),
	)

	var allEvents []MSEvent
	for url != "" {
		resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("fetching calendar view: %w", err)
		}

		var page MSCalendarViewResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding calendar view response: %w", err)
		}
		resp.Body.Close()

		allEvents = append(allEvents, page.Value...)
		url = page.NextLink
	}

	return allEvents, nil
}

// CalendarViewDelta performs a delta sync of calendar events.
// If deltaLink is empty, an initial delta request is made for the given time range.
// Returns the events, the new deltaLink for future calls, and any error.
func (c *MicrosoftCalendarClient) CalendarViewDelta(ctx context.Context, deltaLink string, start, end time.Time) ([]MSEvent, string, error) {
	var url string
	if deltaLink != "" {
		url = deltaLink
	} else {
		url = fmt.Sprintf("%s/me/calendarView/delta?startDateTime=%s&endDateTime=%s",
			c.baseURL,
			start.UTC().Format(time.RFC3339),
			end.UTC().Format(time.RFC3339),
		)
	}

	var allEvents []MSEvent
	var newDeltaLink string

	for url != "" {
		resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", fmt.Errorf("fetching calendar view delta: %w", err)
		}

		var page MSCalendarViewResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, "", fmt.Errorf("decoding delta response: %w", err)
		}
		resp.Body.Close()

		allEvents = append(allEvents, page.Value...)

		if page.NextLink != "" {
			url = page.NextLink
			continue
		}
		if page.DeltaLink != "" {
			newDeltaLink = page.DeltaLink
		}
		break
	}

	return allEvents, newDeltaLink, nil
}

// GetSchedule queries the free/busy schedule for the given email addresses.
func (c *MicrosoftCalendarClient) GetSchedule(ctx context.Context, emails []string, start, end time.Time) ([]MSScheduleItem, error) {
	body := map[string]interface{}{
		"schedules": emails,
		"startTime": MSDateTime{
			DateTime: start.UTC().Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		"endTime": MSDateTime{
			DateTime: end.UTC().Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		"availabilityViewInterval": 30,
	}

	url := fmt.Sprintf("%s/me/calendar/getSchedule", c.baseURL)
	resp, err := c.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("fetching schedule: %w", err)
	}
	defer resp.Body.Close()

	var schedResp MSScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedResp); err != nil {
		return nil, fmt.Errorf("decoding schedule response: %w", err)
	}
	return schedResp.Value, nil
}

// CreateEvent creates a new event on the user's default calendar.
func (c *MicrosoftCalendarClient) CreateEvent(ctx context.Context, event MSEvent) (*MSEvent, error) {
	url := fmt.Sprintf("%s/me/events", c.baseURL)
	resp, err := c.doRequest(ctx, http.MethodPost, url, event)
	if err != nil {
		return nil, fmt.Errorf("creating event: %w", err)
	}
	defer resp.Body.Close()

	var created MSEvent
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decoding created event: %w", err)
	}
	return &created, nil
}

// --- Sync integration ---

// syncMicrosoft performs incremental or full sync of Microsoft Calendar events for the given account.
func (s *SyncEngine) syncMicrosoft(ctx database.TenantContext, accountID uuid.UUID) error {
	state, err := s.getSyncState(ctx, accountID, "microsoft")
	if err != nil {
		return fmt.Errorf("getting sync state: %w", err)
	}

	client, err := s.buildMicrosoftClient(ctx, accountID)
	if err != nil {
		return fmt.Errorf("building microsoft client: %w", err)
	}

	deltaLink, err := s.doMicrosoftSync(ctx, client, accountID, state.SyncToken, false)
	if err != nil {
		return err
	}

	if deltaLink != "" {
		if err := s.saveSyncState(ctx, accountID, "microsoft", deltaLink); err != nil {
			return fmt.Errorf("saving sync state: %w", err)
		}
	}
	return nil
}

// doMicrosoftSync runs the delta sync loop. If retried is true, a delta expiration will not
// trigger another retry (prevents infinite loops).
func (s *SyncEngine) doMicrosoftSync(ctx database.TenantContext, client *MicrosoftCalendarClient,
	accountID uuid.UUID, deltaLink string, retried bool) (string, error) {

	windowStart := time.Now().AddDate(0, 0, -s.cfg.SyncWindowDays)
	windowEnd := time.Now().AddDate(0, 0, s.cfg.SyncWindowDays)

	var events []MSEvent
	var newDeltaLink string

	err := s.retryPolicy.Do(func() error {
		var callErr error
		events, newDeltaLink, callErr = client.CalendarViewDelta(ctx, deltaLink, windowStart, windowEnd)
		return callErr
	})
	if err != nil {
		if isMicrosoftDeltaExpired(err) {
			if retried {
				return "", fmt.Errorf("microsoft calendar delta expired after retry: %w", err)
			}
			slog.Warn("microsoft delta link expired, performing full resync",
				"user_id", ctx.UserID, "account_id", accountID)
			if clearErr := s.clearSyncState(ctx, accountID, "microsoft"); clearErr != nil {
				return "", fmt.Errorf("clearing sync state after delta expiration: %w", clearErr)
			}
			return s.doMicrosoftSync(ctx, client, accountID, "", true)
		}
		return "", fmt.Errorf("microsoft calendar delta sync: %w", err)
	}

	for _, msEvent := range events {
		if msEvent.Removed != nil {
			if delErr := s.deleteEvent(ctx, accountID, msEvent.ID); delErr != nil {
				slog.Error("failed to delete removed microsoft event",
					"event_id", msEvent.ID, "err", delErr)
			}
			continue
		}

		event, normErr := normalizeMSEvent(msEvent, ctx.UserID, ctx.TenantID, accountID)
		if normErr != nil {
			slog.Error("failed to normalize microsoft event",
				"event_id", msEvent.ID, "err", normErr)
			continue
		}

		if upsErr := s.upsertEvent(ctx, event); upsErr != nil {
			slog.Error("failed to upsert microsoft event",
				"event_id", msEvent.ID, "err", upsErr)
		}
	}

	return newDeltaLink, nil
}

// buildMicrosoftClient constructs a MicrosoftCalendarClient from stored OAuth tokens.
func (s *SyncEngine) buildMicrosoftClient(ctx database.TenantContext, accountID uuid.UUID) (*MicrosoftCalendarClient, error) {
	tokenProvider := "microsoft_calendar"

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	var accessToken, refreshToken, email string
	var expiresAt time.Time
	err = conn.QueryRow(ctx,
		`SELECT access_token, refresh_token, expires_at, provider_email
		 FROM calendar_accounts WHERE id = $1`,
		accountID,
	).Scan(&accessToken, &refreshToken, &expiresAt, &email)
	if err != nil {
		return nil, fmt.Errorf("fetching calendar account tokens: %w", err)
	}

	initialToken := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiresAt,
	}

	conf := &oauth2.Config{
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		},
		Scopes: []string{"https://graph.microsoft.com/Calendars.ReadWrite"},
	}

	baseSource := conf.TokenSource(ctx, initialToken)
	persistSource := NewPersistingTokenSource(baseSource, ctx, accountID, tokenProvider, email, s.tokenStore)

	httpClient := oauth2.NewClient(ctx, persistSource)
	return NewMicrosoftCalendarClient(httpClient), nil
}

// normalizeMSEvent converts a Microsoft Graph event to the internal CalendarEvent model.
func normalizeMSEvent(ms MSEvent, userID, tenantID, accountID uuid.UUID) (CalendarEvent, error) {
	event := CalendarEvent{
		UserID:          userID,
		TenantID:        tenantID,
		AccountID:       accountID,
		Provider:        "microsoft",
		ProviderEventID: ms.ID,
		CalendarID:      "primary",
		Title:           ms.Subject,
		Description:     ms.BodyPreview,
		Location:        ms.Location.DisplayName,
		IsAllDay:        ms.IsAllDay,
		IsOnline:        ms.IsOnlineMeeting,
		MeetingURL:      ms.OnlineMeetingUrl,
		LastSynced:      time.Now(),
	}

	// Parse start time.
	startTime, startTZ, err := parseMSDateTime(ms.Start)
	if err != nil {
		return event, fmt.Errorf("parsing start time: %w", err)
	}
	event.StartTime = startTime
	event.TimeZone = startTZ

	// Parse end time.
	endTime, _, err := parseMSDateTime(ms.End)
	if err != nil {
		return event, fmt.Errorf("parsing end time: %w", err)
	}
	event.EndTime = endTime

	// Map showAs to status.
	switch ms.ShowAs {
	case "busy":
		event.Status = "confirmed"
	case "tentative":
		event.Status = "tentative"
	case "free":
		event.Status = "free"
	case "oof":
		event.Status = "outOfOffice"
	default:
		event.Status = "confirmed"
	}

	// Extract organizer email.
	if ms.Organizer.EmailAddress.Address != "" {
		event.OrganizerEmail = ms.Organizer.EmailAddress.Address
	}

	// Build attendees JSONB array.
	if len(ms.Attendees) > 0 {
		type attendee struct {
			Email          string `json:"email"`
			Name           string `json:"name"`
			ResponseStatus string `json:"responseStatus"`
		}
		attendees := make([]attendee, 0, len(ms.Attendees))
		for _, a := range ms.Attendees {
			attendees = append(attendees, attendee{
				Email:          a.EmailAddress.Address,
				Name:           a.EmailAddress.Name,
				ResponseStatus: a.Status.Response,
			})
		}
		data, marshalErr := json.Marshal(attendees)
		if marshalErr != nil {
			return event, fmt.Errorf("marshaling attendees: %w", marshalErr)
		}
		event.Attendees = data
	}

	// Map recurrence to an RRULE-style string.
	if ms.Recurrence != nil {
		event.RecurrenceRule = msRecurrenceToRule(ms.Recurrence)
	}

	return event, nil
}

// parseMSDateTime parses a Microsoft Graph dateTime/timeZone pair into a time.Time.
// Returns the parsed time and the timezone name.
func parseMSDateTime(dt MSDateTime) (time.Time, string, error) {
	if dt.DateTime == "" {
		return time.Time{}, "", fmt.Errorf("empty dateTime")
	}

	// Microsoft Graph returns datetimes in formats like:
	// "2024-01-15T09:00:00.0000000" or "2024-01-15T09:00:00"
	// The TimeZone field specifies the IANA timezone.
	tzName := dt.TimeZone
	if tzName == "" {
		tzName = "UTC"
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Fallback: try UTC if the timezone is not recognized.
		slog.Warn("unrecognized timezone, falling back to UTC",
			"timezone", tzName)
		loc = time.UTC
		tzName = "UTC"
	}

	// Try multiple formats that Microsoft Graph may return.
	formats := []string{
		"2006-01-02T15:04:05.0000000",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		t, parseErr := time.ParseInLocation(format, dt.DateTime, loc)
		if parseErr == nil {
			return t, tzName, nil
		}
	}

	return time.Time{}, "", fmt.Errorf("unable to parse dateTime %q with timezone %q", dt.DateTime, tzName)
}

// msRecurrenceToRule converts Microsoft recurrence pattern to an RRULE-like string.
func msRecurrenceToRule(rec *MSRecurrence) string {
	if rec == nil {
		return ""
	}

	var freq string
	switch rec.Pattern.Type {
	case "daily":
		freq = "DAILY"
	case "weekly":
		freq = "WEEKLY"
	case "absoluteMonthly", "relativeMonthly":
		freq = "MONTHLY"
	case "absoluteYearly", "relativeYearly":
		freq = "YEARLY"
	default:
		return ""
	}

	if rec.Pattern.Interval > 1 {
		return fmt.Sprintf("RRULE:FREQ=%s;INTERVAL=%d", freq, rec.Pattern.Interval)
	}
	return fmt.Sprintf("RRULE:FREQ=%s", freq)
}

// isMicrosoftDeltaExpired checks if an error indicates the delta link has expired.
// Microsoft returns 410 Gone or an error body containing "SyncStateNotFound".
func isMicrosoftDeltaExpired(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "410") {
		return true
	}
	if strings.Contains(s, "SyncStateNotFound") {
		return true
	}
	return false
}
