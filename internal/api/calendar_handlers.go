package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/calendar"
	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// CalendarHandlers handles calendar-related API endpoints.
type CalendarHandlers struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// NewCalendarHandlers creates calendar API handlers.
func NewCalendarHandlers(pool *pgxpool.Pool, rdb *redis.Client) *CalendarHandlers {
	return &CalendarHandlers{pool: pool, rdb: rdb}
}

// HandleEvents returns calendar events for the authenticated user.
// GET /api/v1/calendar/events?start=ISO&end=ISO
func (h *CalendarHandlers) HandleEvents(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	var err error
	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, "INVALID_START", "Invalid start time format, use RFC3339")
			return
		}
	} else {
		start = time.Now().Truncate(24 * time.Hour)
	}
	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, "INVALID_END", "Invalid end time format, use RFC3339")
			return
		}
	} else {
		end = start.Add(7 * 24 * time.Hour)
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	rows, err := conn.Query(r.Context(),
		`SELECT id, user_id, tenant_id, account_id, provider, provider_event_id, calendar_id,
			title, description, start_time, end_time, time_zone, location,
			is_all_day, status, attendees, recurrence_rule, organizer_email,
			is_online, meeting_url, briefed_at, last_synced, created_at, updated_at
		FROM calendar_events
		WHERE user_id = $1 AND start_time < $3 AND end_time > $2 AND status != 'cancelled'
		ORDER BY start_time ASC`, tc.UserID, start, end)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to query events")
		return
	}
	defer rows.Close()

	type eventResponse struct {
		ID             string          `json:"id"`
		Provider       string          `json:"provider"`
		Title          string          `json:"title"`
		Description    string          `json:"description"`
		StartTime      time.Time       `json:"start_time"`
		EndTime        time.Time       `json:"end_time"`
		TimeZone       string          `json:"time_zone"`
		Location       string          `json:"location"`
		IsAllDay       bool            `json:"is_all_day"`
		Status         string          `json:"status"`
		Attendees      json.RawMessage `json:"attendees"`
		OrganizerEmail string          `json:"organizer_email"`
		IsOnline       bool            `json:"is_online"`
		MeetingURL     string          `json:"meeting_url"`
		BriefedAt      *time.Time      `json:"briefed_at"`
	}

	var events []eventResponse
	for rows.Next() {
		var e eventResponse
		var userID, tenantID, accountID, id string
		var providerEventID, calendarID, recurrenceRule string
		var lastSynced, createdAt, updatedAt time.Time
		if err := rows.Scan(
			&id, &userID, &tenantID, &accountID, &e.Provider, &providerEventID, &calendarID,
			&e.Title, &e.Description, &e.StartTime, &e.EndTime, &e.TimeZone, &e.Location,
			&e.IsAllDay, &e.Status, &e.Attendees, &recurrenceRule, &e.OrganizerEmail,
			&e.IsOnline, &e.MeetingURL, &e.BriefedAt, &lastSynced, &createdAt, &updatedAt,
		); err != nil {
			WriteError(w, r, http.StatusInternalServerError, "SCAN_ERROR", "Failed to scan event")
			return
		}
		e.ID = id
		events = append(events, e)
	}
	if events == nil {
		events = []eventResponse{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"data": events,
		"meta": map[string]interface{}{
			"count": len(events),
			"start": start.Format(time.RFC3339),
			"end":   end.Format(time.RFC3339),
		},
	})
}

// HandleConnections returns connected calendar providers for the user.
// GET /api/v1/calendar/connections
func (h *CalendarHandlers) HandleConnections(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	rows, err := conn.Query(r.Context(),
		`SELECT provider, status, last_sync FROM calendar_sync_state WHERE user_id = $1`, tc.UserID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to query connections")
		return
	}
	defer rows.Close()

	type connection struct {
		Provider string     `json:"provider"`
		Status   string     `json:"status"`
		LastSync *time.Time `json:"last_sync"`
	}

	var connections []connection
	for rows.Next() {
		var c connection
		if err := rows.Scan(&c.Provider, &c.Status, &c.LastSync); err != nil {
			continue
		}
		connections = append(connections, c)
	}
	if connections == nil {
		connections = []connection{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"data": connections,
	})
}

// HandleConflicts returns calendar conflicts for the user.
// GET /api/v1/calendar/conflicts?start=ISO&end=ISO
func (h *CalendarHandlers) HandleConflicts(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	rows, err := conn.Query(r.Context(),
		`SELECT id, event_a_id, event_b_id, type, severity, overlap_min, gap_min, detail, resolved
		FROM calendar_conflicts WHERE user_id = $1 AND NOT resolved
		ORDER BY created_at DESC`, tc.UserID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to query conflicts")
		return
	}
	defer rows.Close()

	type conflictResponse struct {
		ID         string  `json:"id"`
		EventAID   string  `json:"event_a_id"`
		EventBID   *string `json:"event_b_id"`
		Type       string  `json:"type"`
		Severity   string  `json:"severity"`
		OverlapMin *int    `json:"overlap_min"`
		GapMin     *int    `json:"gap_min"`
		Detail     string  `json:"detail"`
		Resolved   bool    `json:"resolved"`
	}

	var conflicts []conflictResponse
	for rows.Next() {
		var c conflictResponse
		if err := rows.Scan(&c.ID, &c.EventAID, &c.EventBID, &c.Type, &c.Severity, &c.OverlapMin, &c.GapMin, &c.Detail, &c.Resolved); err != nil {
			continue
		}
		conflicts = append(conflicts, c)
	}
	if conflicts == nil {
		conflicts = []conflictResponse{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": conflicts})
}

// HandleGetPreferences returns user calendar preferences.
// GET /api/v1/calendar/preferences
func (h *CalendarHandlers) HandleGetPreferences(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	var prefs struct {
		PreferredStartHour int             `json:"preferred_start_hour"`
		PreferredEndHour   int             `json:"preferred_end_hour"`
		BufferMinutes      int             `json:"buffer_minutes"`
		NoMeetingDays      json.RawMessage `json:"no_meeting_days"`
		FocusBlocks        json.RawMessage `json:"focus_blocks"`
		HomeTimezone       string          `json:"home_timezone"`
	}

	err = conn.QueryRow(r.Context(),
		`SELECT preferred_start_hour, preferred_end_hour, buffer_minutes, no_meeting_days, focus_blocks, home_timezone
		FROM calendar_preferences WHERE user_id = $1`, tc.UserID,
	).Scan(&prefs.PreferredStartHour, &prefs.PreferredEndHour, &prefs.BufferMinutes,
		&prefs.NoMeetingDays, &prefs.FocusBlocks, &prefs.HomeTimezone)
	if err != nil {
		// Return defaults if no preferences set
		prefs.PreferredStartHour = 9
		prefs.PreferredEndHour = 18
		prefs.BufferMinutes = 15
		prefs.NoMeetingDays = json.RawMessage("[]")
		prefs.FocusBlocks = json.RawMessage("[]")
		prefs.HomeTimezone = "UTC"
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": prefs})
}

// HandleUpdatePreferences updates user calendar preferences.
// PUT /api/v1/calendar/preferences
func (h *CalendarHandlers) HandleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	var req struct {
		PreferredStartHour *int            `json:"preferred_start_hour"`
		PreferredEndHour   *int            `json:"preferred_end_hour"`
		BufferMinutes      *int            `json:"buffer_minutes"`
		NoMeetingDays      json.RawMessage `json:"no_meeting_days"`
		FocusBlocks        json.RawMessage `json:"focus_blocks"`
		HomeTimezone       *string         `json:"home_timezone"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "Invalid request body")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	_, err = conn.Exec(r.Context(),
		`INSERT INTO calendar_preferences (user_id, tenant_id, preferred_start_hour, preferred_end_hour, buffer_minutes, no_meeting_days, focus_blocks, home_timezone)
		VALUES ($1, $2, COALESCE($3, 9), COALESCE($4, 18), COALESCE($5, 15), COALESCE($6, '[]'::jsonb), COALESCE($7, '[]'::jsonb), COALESCE($8, 'UTC'))
		ON CONFLICT (user_id) DO UPDATE SET
			preferred_start_hour = COALESCE($3, calendar_preferences.preferred_start_hour),
			preferred_end_hour = COALESCE($4, calendar_preferences.preferred_end_hour),
			buffer_minutes = COALESCE($5, calendar_preferences.buffer_minutes),
			no_meeting_days = COALESCE($6, calendar_preferences.no_meeting_days),
			focus_blocks = COALESCE($7, calendar_preferences.focus_blocks),
			home_timezone = COALESCE($8, calendar_preferences.home_timezone),
			updated_at = now()`,
		tc.UserID, tc.TenantID, req.PreferredStartHour, req.PreferredEndHour,
		req.BufferMinutes, req.NoMeetingDays, req.FocusBlocks, req.HomeTimezone)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "UPDATE_ERROR", "Failed to update preferences")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleSchedulingRequests returns pending scheduling requests.
// GET /api/v1/calendar/scheduling-requests?status=detected
func (h *CalendarHandlers) HandleSchedulingRequests(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	status := r.URL.Query().Get("status")
	if status == "" {
		status = "detected"
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	rows, err := conn.Query(r.Context(),
		`SELECT id, email_id, confidence, proposed_times, duration_hint, attendees,
			location_preference, urgency, status, suggested_slots, accepted_slot, created_at
		FROM scheduling_requests WHERE user_id = $1 AND status = $2
		ORDER BY created_at DESC LIMIT 50`, tc.UserID, status)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to query scheduling requests")
		return
	}
	defer rows.Close()

	type schedReq struct {
		ID                 string          `json:"id"`
		EmailID            *string         `json:"email_id"`
		Confidence         float64         `json:"confidence"`
		ProposedTimes      json.RawMessage `json:"proposed_times"`
		DurationHint       *int            `json:"duration_hint"`
		Attendees          json.RawMessage `json:"attendees"`
		LocationPreference *string         `json:"location_preference"`
		Urgency            *string         `json:"urgency"`
		Status             string          `json:"status"`
		SuggestedSlots     json.RawMessage `json:"suggested_slots"`
		AcceptedSlot       json.RawMessage `json:"accepted_slot"`
		CreatedAt          time.Time       `json:"created_at"`
	}

	var requests []schedReq
	for rows.Next() {
		var sr schedReq
		if err := rows.Scan(&sr.ID, &sr.EmailID, &sr.Confidence, &sr.ProposedTimes, &sr.DurationHint,
			&sr.Attendees, &sr.LocationPreference, &sr.Urgency, &sr.Status, &sr.SuggestedSlots,
			&sr.AcceptedSlot, &sr.CreatedAt); err != nil {
			continue
		}
		requests = append(requests, sr)
	}
	if requests == nil {
		requests = []schedReq{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": requests})
}

// HandleSuggestSlots finds available time slots for a meeting request.
// POST /api/v1/calendar/suggest-slots
func (h *CalendarHandlers) HandleSuggestSlots(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	var req calendar.SlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "Invalid request body")
		return
	}

	engine := calendar.NewSlotEngine(h.pool, &config.CalendarConfig{
		SlotIncrementMinutes: 15,
		MaxSlotSuggestions:   3,
	})
	slots, err := engine.SuggestSlots(tc, req)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "SLOT_ERROR", "Failed to suggest slots")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": slots})
}

// HandleApproveSlot approves a suggested slot and creates a calendar event.
// POST /api/v1/calendar/approve-slot
func (h *CalendarHandlers) HandleApproveSlot(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	var req struct {
		RequestID string `json:"request_id"`
		SlotIndex int    `json:"slot_index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "Invalid request body")
		return
	}

	requestID, err := uuid.Parse(req.RequestID)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid request_id")
		return
	}

	approval := calendar.NewSlotApproval(h.pool, nil)
	if err := approval.ApproveSlot(tc, requestID, req.SlotIndex); err != nil {
		if strings.Contains(err.Error(), "hard conflict") {
			WriteError(w, r, http.StatusConflict, "CONFLICT", err.Error())
			return
		}
		WriteError(w, r, http.StatusInternalServerError, "APPROVE_ERROR", err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "approved"})
}

// HandleGetBrief returns the meeting prep brief for an event.
// GET /api/v1/calendar/meeting-briefs/{eventID}
func (h *CalendarHandlers) HandleGetBrief(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	eventIDStr := chi.URLParam(r, "eventID")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid event ID")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	var brief struct {
		ID              string          `json:"id"`
		BriefText       string          `json:"brief_text"`
		ModelUsed       string          `json:"model_used"`
		AttendeeContext json.RawMessage `json:"attendee_context"`
		AgendaDetected  string          `json:"agenda_detected"`
		GeneratedAt     time.Time       `json:"generated_at"`
	}
	err = conn.QueryRow(r.Context(),
		`SELECT id, brief_text, model_used, attendee_context, agenda_detected, generated_at
		FROM meeting_briefs WHERE event_id = $1 AND user_id = $2`,
		eventID, tc.UserID,
	).Scan(&brief.ID, &brief.BriefText, &brief.ModelUsed, &brief.AttendeeContext, &brief.AgendaDetected, &brief.GeneratedAt)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "No brief found for this event")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": brief})
}

// HandleRunningLate sends a running late notification to all event attendees.
// POST /api/v1/calendar/running-late/{eventID}
func (h *CalendarHandlers) HandleRunningLate(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	eventIDStr := chi.URLParam(r, "eventID")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid event ID")
		return
	}

	svc := calendar.NewRunningLateService(h.pool)
	if err := svc.SendRunningLate(tc, eventID); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "SEND_ERROR", err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "sent"})
}

// HandleMeetingNotes saves or updates meeting notes for an event.
// POST /api/v1/calendar/meeting-notes/{eventID}
func (h *CalendarHandlers) HandleMeetingNotes(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	eventIDStr := chi.URLParam(r, "eventID")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid event ID")
		return
	}

	var req struct {
		Notes     string   `json:"notes"`
		Outcome   string   `json:"outcome"`
		Followups []string `json:"followup_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "Invalid request body")
		return
	}

	engine := calendar.NewNotesEngine(h.pool, &config.CalendarConfig{NotesPromptDelayMinutes: 5})
	if err := engine.SaveNotes(tc, eventID, req.Notes, req.Outcome, req.Followups); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "SAVE_ERROR", err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "saved"})
}
