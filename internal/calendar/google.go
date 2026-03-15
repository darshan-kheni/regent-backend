package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// BusyInterval represents a time range during which a calendar is busy.
type BusyInterval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// GoogleCalendarClient wraps the Google Calendar v3 API service.
type GoogleCalendarClient struct {
	svc *gcal.Service
}

// NewGoogleCalendarClient creates a GoogleCalendarClient using the provided HTTP client
// (which should carry OAuth2 credentials).
func NewGoogleCalendarClient(httpClient *http.Client) (*GoogleCalendarClient, error) {
	svc, err := gcal.NewService(context.Background(), option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("creating google calendar service: %w", err)
	}
	return &GoogleCalendarClient{svc: svc}, nil
}

// syncGoogle performs an incremental or full sync of Google Calendar events for the given account.
func (s *SyncEngine) syncGoogle(ctx database.TenantContext, accountID uuid.UUID) error {
	state, err := s.getSyncState(ctx, accountID, "google")
	if err != nil {
		return fmt.Errorf("getting sync state: %w", err)
	}

	client, err := s.buildGoogleClient(ctx, accountID)
	if err != nil {
		return fmt.Errorf("building google client: %w", err)
	}

	syncToken, err := s.doGoogleSync(ctx, client, accountID, state.SyncToken, false)
	if err != nil {
		return err
	}

	if syncToken != "" {
		if err := s.saveSyncState(ctx, accountID, "google", syncToken); err != nil {
			return fmt.Errorf("saving sync state: %w", err)
		}
	}
	return nil
}

// doGoogleSync runs the actual sync loop. If retried is true, a 410 Gone will not trigger
// another retry (prevents infinite loops).
func (s *SyncEngine) doGoogleSync(ctx database.TenantContext, client *GoogleCalendarClient,
	accountID uuid.UUID, syncToken string, retried bool) (string, error) {

	var nextSyncToken string
	pageToken := ""

	for {
		req := client.svc.Events.List("primary").
			SingleEvents(true).
			MaxResults(250).
			Context(ctx)

		if syncToken != "" {
			// Incremental sync using sync token.
			req = req.SyncToken(syncToken)
		} else {
			// Full sync: fetch events from last N days.
			windowStart := time.Now().AddDate(0, 0, -s.cfg.SyncWindowDays)
			req = req.TimeMin(windowStart.Format(time.RFC3339))
		}

		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		var events *gcal.Events
		err := s.retryPolicy.Do(func() error {
			var callErr error
			events, callErr = req.Do()
			return callErr
		})
		if err != nil {
			if isGoogleGone(err) {
				if retried {
					return "", fmt.Errorf("google calendar 410 Gone after retry: %w", err)
				}
				slog.Warn("google sync token expired (410), performing full resync",
					"user_id", ctx.UserID, "account_id", accountID)
				if clearErr := s.clearSyncState(ctx, accountID, "google"); clearErr != nil {
					return "", fmt.Errorf("clearing sync state after 410: %w", clearErr)
				}
				return s.doGoogleSync(ctx, client, accountID, "", true)
			}
			return "", fmt.Errorf("listing google events: %w", err)
		}

		for _, item := range events.Items {
			if item.Status == "cancelled" {
				if delErr := s.deleteEvent(ctx, accountID, item.Id); delErr != nil {
					slog.Error("failed to delete cancelled event",
						"event_id", item.Id, "err", delErr)
				}
				continue
			}

			event, normErr := normalizeGoogleEvent(item, ctx.UserID, ctx.TenantID, accountID)
			if normErr != nil {
				slog.Error("failed to normalize google event",
					"event_id", item.Id, "err", normErr)
				continue
			}

			if upsErr := s.upsertEvent(ctx, event); upsErr != nil {
				slog.Error("failed to upsert google event",
					"event_id", item.Id, "err", upsErr)
			}
		}

		if events.NextPageToken != "" {
			pageToken = events.NextPageToken
			continue
		}

		if events.NextSyncToken != "" {
			nextSyncToken = events.NextSyncToken
		}
		break
	}

	return nextSyncToken, nil
}

// buildGoogleClient constructs a GoogleCalendarClient from stored OAuth tokens.
// It retrieves decrypted tokens via TokenGetter, wraps them with PersistingTokenSource
// for automatic refresh persistence, and returns a ready-to-use client.
func (s *SyncEngine) buildGoogleClient(ctx database.TenantContext, accountID uuid.UUID) (*GoogleCalendarClient, error) {
	if s.tokenGetter == nil {
		return nil, fmt.Errorf("token getter not configured")
	}
	if s.googleCreds == nil {
		return nil, fmt.Errorf("google oauth credentials not configured")
	}

	// Retrieve decrypted tokens from the store. Calendar uses "google_calendar" provider key.
	tokens, err := s.tokenGetter.GetTokens(ctx, ctx.UserID, "google_calendar")
	if err != nil {
		return nil, fmt.Errorf("fetching oauth tokens: %w", err)
	}

	oauthCfg := &oauth2.Config{
		ClientID:     s.googleCreds.ClientID,
		ClientSecret: s.googleCreds.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{"https://www.googleapis.com/auth/calendar"},
	}

	initialToken := &oauth2.Token{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		Expiry:       tokens.ExpiresAt,
	}

	baseSource := oauthCfg.TokenSource(ctx, initialToken)
	persistSource := NewPersistingTokenSource(
		baseSource, ctx, accountID,
		"google_calendar", tokens.ProviderEmail, s.tokenStore,
	)

	httpClient := oauth2.NewClient(ctx, persistSource)
	return NewGoogleCalendarClient(httpClient)
}

// normalizeGoogleEvent converts a Google Calendar event to the internal CalendarEvent model.
func normalizeGoogleEvent(item *gcal.Event, userID, tenantID, accountID uuid.UUID) (CalendarEvent, error) {
	event := CalendarEvent{
		UserID:          userID,
		TenantID:        tenantID,
		AccountID:       accountID,
		Provider:        "google",
		ProviderEventID: item.Id,
		CalendarID:      "primary",
		Title:           item.Summary,
		Description:     item.Description,
		Location:        item.Location,
		LastSynced:      time.Now(),
	}

	// Parse start/end times, handling both timed and all-day events.
	if item.Start != nil {
		if item.Start.DateTime != "" {
			t, err := time.Parse(time.RFC3339, item.Start.DateTime)
			if err != nil {
				return event, fmt.Errorf("parsing start datetime: %w", err)
			}
			event.StartTime = t
			event.IsAllDay = false
		} else if item.Start.Date != "" {
			t, err := time.Parse("2006-01-02", item.Start.Date)
			if err != nil {
				return event, fmt.Errorf("parsing start date: %w", err)
			}
			event.StartTime = t
			event.IsAllDay = true
		}
		if item.Start.TimeZone != "" {
			event.TimeZone = item.Start.TimeZone
		}
	}

	if item.End != nil {
		if item.End.DateTime != "" {
			t, err := time.Parse(time.RFC3339, item.End.DateTime)
			if err != nil {
				return event, fmt.Errorf("parsing end datetime: %w", err)
			}
			event.EndTime = t
		} else if item.End.Date != "" {
			t, err := time.Parse("2006-01-02", item.End.Date)
			if err != nil {
				return event, fmt.Errorf("parsing end date: %w", err)
			}
			event.EndTime = t
		}
	}

	// Map status: Google uses "confirmed", "tentative", "cancelled".
	switch item.Status {
	case "confirmed":
		event.Status = "confirmed"
	case "tentative":
		event.Status = "tentative"
	case "cancelled":
		event.Status = "cancelled"
	default:
		event.Status = "confirmed"
	}

	// Extract organizer email.
	if item.Organizer != nil {
		event.OrganizerEmail = item.Organizer.Email
	}

	// Build attendees JSONB array.
	if len(item.Attendees) > 0 {
		type attendeeEntry struct {
			Email          string `json:"email"`
			Name           string `json:"name"`
			ResponseStatus string `json:"responseStatus"`
		}
		attendees := make([]attendeeEntry, 0, len(item.Attendees))
		for _, a := range item.Attendees {
			attendees = append(attendees, attendeeEntry{
				Email:          a.Email,
				Name:           a.DisplayName,
				ResponseStatus: a.ResponseStatus,
			})
		}
		data, err := json.Marshal(attendees)
		if err != nil {
			return event, fmt.Errorf("marshaling attendees: %w", err)
		}
		event.Attendees = data
	}

	// Extract recurrence rule (first RRULE if present).
	if len(item.Recurrence) > 0 {
		for _, r := range item.Recurrence {
			if strings.HasPrefix(r, "RRULE:") {
				event.RecurrenceRule = r
				break
			}
		}
	}

	// Extract meeting URL from ConferenceData, HangoutLink, or location.
	meetingURL := extractMeetingURL(item)
	if meetingURL != "" {
		event.MeetingURL = meetingURL
		event.IsOnline = true
	}

	return event, nil
}

// extractMeetingURL attempts to find a video meeting URL from conference data or location.
func extractMeetingURL(item *gcal.Event) string {
	// Prefer structured conference data.
	if item.ConferenceData != nil {
		for _, ep := range item.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && ep.Uri != "" {
				return ep.Uri
			}
		}
	}
	// Check hangoutLink field.
	if item.HangoutLink != "" {
		return item.HangoutLink
	}
	// Fallback: check if location contains a URL.
	if item.Location != "" {
		loc := strings.TrimSpace(item.Location)
		if strings.HasPrefix(loc, "https://") || strings.HasPrefix(loc, "http://") {
			return loc
		}
	}
	return ""
}

// isGoogleGone returns true if the error is a Google API 410 Gone response,
// indicating the sync token has expired.
func isGoogleGone(err error) bool {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 410
	}
	return false
}

// FreeBusy queries Google Calendar's FreeBusy API for the given email addresses
// within the specified time range.
func (g *GoogleCalendarClient) FreeBusy(ctx context.Context, emails []string, start, end time.Time) (map[string][]BusyInterval, error) {
	items := make([]*gcal.FreeBusyRequestItem, 0, len(emails))
	for _, email := range emails {
		items = append(items, &gcal.FreeBusyRequestItem{Id: email})
	}

	req := &gcal.FreeBusyRequest{
		TimeMin: start.Format(time.RFC3339),
		TimeMax: end.Format(time.RFC3339),
		Items:   items,
	}

	resp, err := g.svc.Freebusy.Query(req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("querying google freebusy: %w", err)
	}

	result := make(map[string][]BusyInterval, len(resp.Calendars))
	for calEmail, cal := range resp.Calendars {
		intervals := make([]BusyInterval, 0, len(cal.Busy))
		for _, b := range cal.Busy {
			bStart, parseErr := time.Parse(time.RFC3339, b.Start)
			if parseErr != nil {
				continue
			}
			bEnd, parseErr := time.Parse(time.RFC3339, b.End)
			if parseErr != nil {
				continue
			}
			intervals = append(intervals, BusyInterval{Start: bStart, End: bEnd})
		}
		result[calEmail] = intervals
	}
	return result, nil
}

// CreateEvent creates an event on the user's primary Google Calendar and returns the provider event ID.
func (g *GoogleCalendarClient) CreateEvent(ctx context.Context, event CalendarEvent) (string, error) {
	gEvent := &gcal.Event{
		Summary:     event.Title,
		Description: event.Description,
		Location:    event.Location,
	}

	if event.IsAllDay {
		gEvent.Start = &gcal.EventDateTime{Date: event.StartTime.Format("2006-01-02")}
		gEvent.End = &gcal.EventDateTime{Date: event.EndTime.Format("2006-01-02")}
	} else {
		tz := event.TimeZone
		if tz == "" {
			tz = "UTC"
		}
		gEvent.Start = &gcal.EventDateTime{
			DateTime: event.StartTime.Format(time.RFC3339),
			TimeZone: tz,
		}
		gEvent.End = &gcal.EventDateTime{
			DateTime: event.EndTime.Format(time.RFC3339),
			TimeZone: tz,
		}
	}

	// Add attendees if present.
	if len(event.Attendees) > 0 {
		type attendeeEntry struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		var attendees []attendeeEntry
		if err := json.Unmarshal(event.Attendees, &attendees); err == nil {
			for _, a := range attendees {
				gEvent.Attendees = append(gEvent.Attendees, &gcal.EventAttendee{
					Email:       a.Email,
					DisplayName: a.Name,
				})
			}
		}
	}

	created, err := g.svc.Events.Insert("primary", gEvent).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("creating google event: %w", err)
	}
	return created.Id, nil
}
