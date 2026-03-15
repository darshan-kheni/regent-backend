package tasks

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// ReminderScheduler manages task reminders.
type ReminderScheduler struct {
	pool *pgxpool.Pool
}

// NewReminderScheduler creates a new ReminderScheduler.
func NewReminderScheduler(pool *pgxpool.Pool) *ReminderScheduler {
	return &ReminderScheduler{pool: pool}
}

// Schedule creates 48h, 24h, and 2h reminder entries for a task with a deadline.
func (s *ReminderScheduler) Schedule(ctx database.TenantContext, task *Task) {
	if task.Deadline == nil || s.pool == nil {
		return
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		slog.Debug("reminders: failed to acquire connection", "error", err)
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		slog.Debug("reminders: failed to set RLS", "error", err)
		return
	}

	reminders := []struct {
		reminderType string
		offset       time.Duration
	}{
		{"48h", -48 * time.Hour},
		{"24h", -24 * time.Hour},
		{"2h", -2 * time.Hour},
	}

	now := time.Now()
	for _, r := range reminders {
		scheduledAt := task.Deadline.Add(r.offset)

		// Skip reminders that are already in the past
		if scheduledAt.Before(now) {
			continue
		}

		_, err := conn.Exec(ctx,
			`INSERT INTO task_reminders (task_id, user_id, tenant_id, reminder_type, scheduled_at)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (task_id, reminder_type) DO NOTHING`,
			task.ID, task.UserID, task.TenantID, r.reminderType, scheduledAt,
		)
		if err != nil {
			slog.Warn("reminders: failed to create reminder",
				"task_id", task.ID,
				"type", r.reminderType,
				"error", fmt.Errorf("inserting reminder: %w", err),
			)
		}
	}
}

// CheckAndSend finds pending reminders and sends them via PublishNotificationEvent.
// Called from the task_reminders cron job (every 15 min).
func (s *ReminderScheduler) CheckAndSend(ctx database.TenantContext) error {
	if s.pool == nil {
		return nil
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT r.id, r.task_id, r.reminder_type, t.title, t.deadline
		 FROM task_reminders r
		 JOIN tasks t ON t.id = r.task_id
		 WHERE r.user_id = $1
		   AND r.sent_at IS NULL
		   AND r.scheduled_at <= now()
		   AND t.status NOT IN ('done', 'dismissed')
		 ORDER BY r.scheduled_at`,
		ctx.UserID,
	)
	if err != nil {
		return fmt.Errorf("querying pending reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var reminderID, taskID interface{}
		var reminderType, title string
		var deadline *time.Time
		if err := rows.Scan(&reminderID, &taskID, &reminderType, &title, &deadline); err != nil {
			slog.Warn("reminders: failed to scan row", "error", err)
			continue
		}

		// TODO: Wire to briefings.PublishNotificationEvent() when available
		// For now, just log the reminder
		slog.Info("reminders: sending task reminder",
			"user_id", ctx.UserID,
			"task_title", title,
			"reminder_type", reminderType,
			"deadline", deadline,
		)

		// Mark as sent
		_, err := conn.Exec(ctx,
			`UPDATE task_reminders SET sent_at = now() WHERE id = $1`,
			reminderID,
		)
		if err != nil {
			slog.Warn("reminders: failed to mark as sent",
				"reminder_id", reminderID,
				"error", fmt.Errorf("updating sent_at: %w", err),
			)
		}
	}

	return nil
}
