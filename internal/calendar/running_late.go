package calendar

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunningLateService handles sending "running late" notifications to meeting attendees.
type RunningLateService struct {
	pool *pgxpool.Pool
}

// NewRunningLateService creates a RunningLateService.
func NewRunningLateService(pool *pgxpool.Pool) *RunningLateService {
	return &RunningLateService{pool: pool}
}

// SendRunningLate sends a running late email to all attendees of the given event.
func (rls *RunningLateService) SendRunningLate(ctx database.TenantContext, eventID uuid.UUID) error {
	event, err := rls.getEvent(ctx, eventID)
	if err != nil {
		return fmt.Errorf("event not found: %w", err)
	}

	emails := extractAttendeeEmails(event.Attendees)
	if len(emails) == 0 {
		return fmt.Errorf("no attendees to notify")
	}

	subject := fmt.Sprintf("Running late — %s", event.Title)
	body := fmt.Sprintf("Hi all, running a few minutes late to our %s meeting. Will join shortly.", event.Title)

	slog.Info("sending running late notification",
		"event_id", eventID,
		"attendees", len(emails),
		"subject", subject,
	)

	// TODO: Wire to Phase 3 email engine to actually send
	_ = body
	_ = subject

	return nil
}

func (rls *RunningLateService) getEvent(ctx database.TenantContext, eventID uuid.UUID) (*CalendarEvent, error) {
	conn, err := rls.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, err
	}

	var e CalendarEvent
	err = conn.QueryRow(ctx,
		`SELECT id, user_id, tenant_id, title, attendees, meeting_url
		FROM calendar_events WHERE id = $1 AND user_id = $2`,
		eventID, ctx.UserID,
	).Scan(&e.ID, &e.UserID, &e.TenantID, &e.Title, &e.Attendees, &e.MeetingURL)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func extractAttendeeEmails(attendeesJSON json.RawMessage) []string {
	if attendeesJSON == nil {
		return nil
	}
	var attendees []struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(attendeesJSON, &attendees); err != nil {
		return nil
	}
	emails := make([]string, 0, len(attendees))
	for _, a := range attendees {
		if a.Email != "" {
			emails = append(emails, a.Email)
		}
	}
	return emails
}
