package tasks

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// CalendarBlocker auto-creates calendar events for tasks with deadlines.
type CalendarBlocker struct {
	pool *pgxpool.Pool
}

// NewCalendarBlocker creates a new CalendarBlocker.
func NewCalendarBlocker(pool *pgxpool.Pool) *CalendarBlocker {
	return &CalendarBlocker{pool: pool}
}

// CreateEventForTask creates a calendar event for a task with a deadline.
// Uses Phase 9 slot engine to find optimal time before deadline.
// Only creates for high-confidence tasks with parsed deadlines.
func (b *CalendarBlocker) CreateEventForTask(ctx database.TenantContext, task *Task) error {
	if task.Deadline == nil || task.Confidence < 0.8 {
		return nil
	}

	// TODO: Wire to Phase 9 calendar.SlotEngine.SuggestSlots() and calendar.EventCreator
	// For now, log the intent
	slog.Info("calendar_block: would create event for task",
		"user_id", ctx.UserID,
		"task_id", task.ID,
		"title", task.Title,
		"deadline", task.Deadline,
	)

	return nil
}

// LinkCalendarEvent links an existing calendar event to a task.
func (b *CalendarBlocker) LinkCalendarEvent(ctx database.TenantContext, task *Task) error {
	if task.CalendarEventID == nil || b.pool == nil {
		return nil
	}

	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS: %w", err)
	}

	_, err = conn.Exec(ctx,
		`UPDATE tasks SET calendar_event_id = $1 WHERE id = $2`,
		task.CalendarEventID, task.ID,
	)
	return err
}
