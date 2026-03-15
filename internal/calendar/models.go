package calendar

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CalendarEvent represents a normalized calendar event from any provider.
type CalendarEvent struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	AccountID       uuid.UUID       `json:"account_id"`
	Provider        string          `json:"provider"`
	ProviderEventID string          `json:"provider_event_id"`
	CalendarID      string          `json:"calendar_id"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	StartTime       time.Time       `json:"start_time"`
	EndTime         time.Time       `json:"end_time"`
	TimeZone        string          `json:"time_zone"`
	Location        string          `json:"location"`
	IsAllDay        bool            `json:"is_all_day"`
	Status          string          `json:"status"`
	Attendees       json.RawMessage `json:"attendees"`
	RecurrenceRule  string          `json:"recurrence_rule"`
	OrganizerEmail  string          `json:"organizer_email"`
	IsOnline        bool            `json:"is_online"`
	MeetingURL      string          `json:"meeting_url"`
	BriefedAt       *time.Time      `json:"briefed_at"`
	LastSynced      time.Time       `json:"last_synced"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// SyncState tracks incremental sync tokens per account+provider.
type SyncState struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	AccountID uuid.UUID `json:"account_id"`
	Provider  string    `json:"provider"`
	SyncToken string    `json:"sync_token"`
	Status    string    `json:"status"`
	LastSync  time.Time `json:"last_sync"`
}

// Conflict represents a detected scheduling conflict between events.
type Conflict struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	EventAID   uuid.UUID  `json:"event_a_id"`
	EventBID   *uuid.UUID `json:"event_b_id,omitempty"`
	Type       string     `json:"type"`
	Severity   string     `json:"severity"`
	OverlapMin int        `json:"overlap_min,omitempty"`
	GapMin     int        `json:"gap_min,omitempty"`
	Detail     string     `json:"detail"`
	Resolved   bool       `json:"resolved"`
}

// CalendarPreference stores user scheduling preferences.
type CalendarPreference struct {
	ID                 uuid.UUID       `json:"id"`
	UserID             uuid.UUID       `json:"user_id"`
	TenantID           uuid.UUID       `json:"tenant_id"`
	PreferredStartHour int             `json:"preferred_start_hour"`
	PreferredEndHour   int             `json:"preferred_end_hour"`
	BufferMinutes      int             `json:"buffer_minutes"`
	NoMeetingDays      json.RawMessage `json:"no_meeting_days"`
	FocusBlocks        json.RawMessage `json:"focus_blocks"`
	HomeTimezone       string          `json:"home_timezone"`
}

// SchedulingRequest represents an AI-detected scheduling intent from an email.
type SchedulingRequest struct {
	ID                 uuid.UUID       `json:"id"`
	UserID             uuid.UUID       `json:"user_id"`
	TenantID           uuid.UUID       `json:"tenant_id"`
	EmailID            *uuid.UUID      `json:"email_id"`
	Confidence         float64         `json:"confidence"`
	ProposedTimes      json.RawMessage `json:"proposed_times"`
	DurationHint       int             `json:"duration_hint"`
	Attendees          json.RawMessage `json:"attendees"`
	LocationPreference string          `json:"location_preference"`
	Urgency            string          `json:"urgency"`
	Status             string          `json:"status"`
	SuggestedSlots     json.RawMessage `json:"suggested_slots"`
	AcceptedSlot       json.RawMessage `json:"accepted_slot"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// SchedulingAnalysis is the AI output from analyzing an email for scheduling intent.
type SchedulingAnalysis struct {
	HasIntent          bool           `json:"has_scheduling_intent"`
	Confidence         float64        `json:"confidence"`
	ProposedTimes      []ProposedTime `json:"proposed_times,omitempty"`
	DurationHint       int            `json:"duration_hint,omitempty"`
	Attendees          []string       `json:"attendees,omitempty"`
	LocationPreference string         `json:"location_preference,omitempty"`
	Urgency            string         `json:"urgency,omitempty"`
}

// ProposedTime represents a time reference extracted from email text.
type ProposedTime struct {
	Text   string `json:"text"`
	Parsed string `json:"parsed,omitempty"`
}

// MeetingBrief is an AI-generated pre-meeting context brief.
type MeetingBrief struct {
	ID              uuid.UUID       `json:"id"`
	EventID         uuid.UUID       `json:"event_id"`
	UserID          uuid.UUID       `json:"user_id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	BriefText       string          `json:"brief_text"`
	ModelUsed       string          `json:"model_used"`
	TokensUsed      int             `json:"tokens_used"`
	AttendeeContext json.RawMessage `json:"attendee_context"`
	AgendaDetected  string          `json:"agenda_detected"`
	GeneratedAt     time.Time       `json:"generated_at"`
}

// MeetingNote stores post-meeting notes and follow-up items.
type MeetingNote struct {
	ID            uuid.UUID       `json:"id"`
	EventID       uuid.UUID       `json:"event_id"`
	UserID        uuid.UUID       `json:"user_id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	Notes         string          `json:"notes"`
	Outcome       string          `json:"outcome"`
	FollowupItems json.RawMessage `json:"followup_items"`
	CreatedAt     time.Time       `json:"created_at"`
}

// SlotRequest is the input for the smart slot suggestion engine.
type SlotRequest struct {
	Attendees       []string  `json:"attendees"`
	DurationMinutes int       `json:"duration_minutes"`
	PreferredStart  time.Time `json:"preferred_start"`
	PreferredEnd    time.Time `json:"preferred_end"`
	MeetingType     string    `json:"meeting_type"`
	LocationPref    string    `json:"location_preference"`
}

// SuggestedSlot is a ranked time slot recommendation.
type SuggestedSlot struct {
	Start                time.Time         `json:"start"`
	End                  time.Time         `json:"end"`
	Score                float64           `json:"score"`
	Reasoning            string            `json:"reasoning"`
	AttendeeAvailability map[string]string `json:"attendee_availability"`
}

// ScoreBreakdown explains the scoring factors for a suggested slot.
type ScoreBreakdown struct {
	BusinessHours       float64 `json:"business_hours"`
	UserPreference      float64 `json:"user_preference"`
	TimeOfDay           float64 `json:"time_of_day"`
	AttendeeConfidence  float64 `json:"attendee_confidence"`
	ProximityToProposed float64 `json:"proximity_to_proposed"`
}

// CalendarProviderMap translates token store provider names to DB provider names.
// Token store uses "google_calendar"/"microsoft_calendar" to distinguish from email OAuth.
// DB and sync logic use "google"/"microsoft".
var CalendarProviderMap = map[string]string{
	"google_calendar":    "google",
	"microsoft_calendar": "microsoft",
}

// AttendeeContext holds enriched contact data for a meeting attendee.
type AttendeeContext struct {
	Name             string   `json:"name"`
	Email            string   `json:"email"`
	Company          string   `json:"company"`
	InteractionCount int      `json:"interaction_count"`
	DominantTone     string   `json:"dominant_tone"`
	LastInteraction  string   `json:"last_interaction"`
	RecentThreads    []string `json:"recent_threads"`
}

// DateTimeResult is a parsed date/time from natural language.
type DateTimeResult struct {
	Start    time.Time
	End      *time.Time
	HasTime  bool
	Duration *time.Duration
}

// FocusBlock represents a user's protected time block.
type FocusBlock struct {
	Start string `json:"start"` // "14:00"
	End   string `json:"end"`   // "16:00"
	Label string `json:"label"` // "Deep Work"
}
