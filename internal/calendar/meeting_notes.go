package calendar

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotesEngine handles post-meeting note prompts and CRUD.
type NotesEngine struct {
	pool *pgxpool.Pool
	cfg  *config.CalendarConfig
}

// NewNotesEngine creates a NotesEngine.
func NewNotesEngine(pool *pgxpool.Pool, cfg *config.CalendarConfig) *NotesEngine {
	return &NotesEngine{pool: pool, cfg: cfg}
}

// PromptForNotes checks for recently ended meetings and prompts for notes.
func (n *NotesEngine) PromptForNotes(ctx database.TenantContext) error {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	delayMinutes := n.cfg.NotesPromptDelayMinutes + 1
	rows, err := conn.Query(ctx,
		`SELECT ce.id, ce.title, ce.attendees, ce.end_time
		FROM calendar_events ce
		WHERE ce.user_id = $1
		  AND ce.end_time BETWEEN now() - ($2 * interval '1 minute') AND now() - (($2 - 1) * interval '1 minute')
		  AND ce.status != 'cancelled'
		  AND ce.id NOT IN (SELECT event_id FROM meeting_notes WHERE user_id = $1)`,
		ctx.UserID, delayMinutes)
	if err != nil {
		return fmt.Errorf("querying ended meetings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventID uuid.UUID
		var title string
		var attendees json.RawMessage
		var endTime time.Time
		if err := rows.Scan(&eventID, &title, &attendees, &endTime); err != nil {
			slog.Error("scan ended meeting", "err", err)
			continue
		}

		slog.Info("prompting for meeting notes",
			"event_id", eventID,
			"title", title,
			"ended_at", endTime,
		)

		// TODO: Send notification via Phase 5 briefings.PublishNotificationEvent()
		// with "Add Notes" and "Remind Me Later" actions
	}

	return nil
}

// SaveNotes stores post-meeting notes for an event.
func (n *NotesEngine) SaveNotes(ctx database.TenantContext, eventID uuid.UUID, notes string, outcome string, followups []string) error {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	followupsJSON, _ := json.Marshal(followups)

	_, err = conn.Exec(ctx,
		`INSERT INTO meeting_notes (event_id, user_id, tenant_id, notes, outcome, followup_items)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (event_id, user_id) DO UPDATE SET
			notes = EXCLUDED.notes,
			outcome = EXCLUDED.outcome,
			followup_items = EXCLUDED.followup_items`,
		eventID, ctx.UserID, ctx.TenantID, notes, outcome, followupsJSON)
	if err != nil {
		return fmt.Errorf("saving meeting notes: %w", err)
	}

	return nil
}

// GetNotes retrieves meeting notes for a specific event.
func (n *NotesEngine) GetNotes(ctx database.TenantContext, eventID uuid.UUID) (*MeetingNote, error) {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, err
	}

	var note MeetingNote
	err = conn.QueryRow(ctx,
		`SELECT id, event_id, user_id, tenant_id, notes, outcome, followup_items, created_at
		FROM meeting_notes WHERE event_id = $1 AND user_id = $2`,
		eventID, ctx.UserID,
	).Scan(&note.ID, &note.EventID, &note.UserID, &note.TenantID,
		&note.Notes, &note.Outcome, &note.FollowupItems, &note.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &note, nil
}
