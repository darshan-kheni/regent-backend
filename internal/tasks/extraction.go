package tasks

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// TaskExtractor processes AI-extracted tasks from email summarization.
type TaskExtractor struct {
	pool     *pgxpool.Pool
	dedup    *DedupChecker
	deadline *DeadlineParser
	priority *PriorityScorer
	reminder *ReminderScheduler
	recur    *RecurringDetector
}

// NewTaskExtractor creates a new TaskExtractor with all sub-components.
func NewTaskExtractor(pool *pgxpool.Pool) *TaskExtractor {
	return &TaskExtractor{
		pool:     pool,
		dedup:    NewDedupChecker(pool),
		deadline: NewDeadlineParser(),
		priority: NewPriorityScorer(pool),
		reminder: NewReminderScheduler(pool),
		recur:    NewRecurringDetector(pool),
	}
}

// ProcessTasks processes extracted tasks from the AI summarization result.
// For each task: filter by confidence, dedup check, parse deadline, score priority, insert, schedule reminders.
func (e *TaskExtractor) ProcessTasks(
	ctx database.TenantContext,
	email models.Email,
	extracted []ExtractedTask,
) error {
	for _, raw := range extracted {
		// Discard low-confidence extractions
		if raw.Confidence < 0.5 {
			continue
		}

		// Dedup check
		if e.dedup.IsDuplicate(ctx, email.UserID, email.ID, raw.Description) {
			slog.Debug("task: skipping duplicate", "title", truncate(raw.Description, 50))
			continue
		}

		// Parse deadline
		deadline := e.deadline.Parse(raw.DeadlineText, email.ReceivedAt)

		// Score priority
		priority := e.priority.Score(ctx, PriorityInput{
			DeadlineText: raw.DeadlineText,
			Deadline:     deadline,
			SenderEmail:  email.FromAddress,
			PriorityHint: raw.PriorityHint,
			UserID:       email.UserID,
		})

		// Set needs_confirmation if deadline text present but could not be parsed
		needsConfirm := deadline == nil && raw.DeadlineText != ""

		t := &Task{
			UserID:            email.UserID,
			TenantID:          email.TenantID,
			EmailID:           &email.ID,
			Title:             truncate(raw.Description, 100),
			Description:       raw.Description,
			Type:              raw.Type,
			Status:            StatusToDo,
			Priority:          priority,
			Deadline:          deadline,
			DeadlineText:      raw.DeadlineText,
			NeedsConfirmation: needsConfirm,
			AssigneeEmail:     raw.Assignee,
			Confidence:        raw.Confidence,
			SourceSubject:     email.Subject,
			SourceSender:      email.FromAddress,
		}

		if err := e.insertTask(ctx, t); err != nil {
			return fmt.Errorf("inserting task: %w", err)
		}

		// Schedule reminders for tasks with deadlines
		if deadline != nil {
			e.reminder.Schedule(ctx, t)
		}

		// Check for recurring patterns
		e.recur.Check(ctx, t)

		slog.Info("task: extracted",
			"user_id", email.UserID,
			"title", t.Title,
			"priority", t.Priority,
			"confidence", t.Confidence,
		)
	}
	return nil
}

func (e *TaskExtractor) insertTask(ctx database.TenantContext, t *Task) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	return conn.QueryRow(ctx,
		`INSERT INTO tasks (user_id, tenant_id, email_id, title, description, type, status,
			priority, deadline, deadline_text, needs_confirmation, assignee_email,
			confidence, source_subject, source_sender)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 RETURNING id`,
		t.UserID, t.TenantID, t.EmailID, t.Title, t.Description, t.Type, t.Status,
		t.Priority, t.Deadline, t.DeadlineText, t.NeedsConfirmation, t.AssigneeEmail,
		t.Confidence, t.SourceSubject, t.SourceSender,
	).Scan(&t.ID)
}

// OverdueUpgrade upgrades overdue tasks to P0. Called from the task_reminders cron job.
func (e *TaskExtractor) OverdueUpgrade(ctx database.TenantContext) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS: %w", err)
	}

	result, err := conn.Exec(ctx,
		`UPDATE tasks SET priority = $1
		 WHERE user_id = $2
		   AND deadline < now()
		   AND status NOT IN ('done', 'dismissed')
		   AND priority != $1`,
		PriorityP0, ctx.UserID,
	)
	if err != nil {
		return fmt.Errorf("upgrading overdue tasks: %w", err)
	}

	if result.RowsAffected() > 0 {
		slog.Info("task: upgraded overdue tasks to P0",
			"user_id", ctx.UserID,
			"count", result.RowsAffected(),
		)
	}
	return nil
}

// DismissTask dismisses a task and records feedback for the AI feedback loop.
func (e *TaskExtractor) DismissTask(ctx database.TenantContext, taskID uuid.UUID) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS: %w", err)
	}

	// Get task info for feedback before dismissing
	var task Task
	err = conn.QueryRow(ctx,
		`SELECT id, title, COALESCE(description, ''), COALESCE(source_sender, ''), COALESCE(source_subject, ''), COALESCE(type, '')
		 FROM tasks WHERE id = $1 AND user_id = $2`,
		taskID, ctx.UserID,
	).Scan(&task.ID, &task.Title, &task.Description, &task.SourceSender, &task.SourceSubject, &task.Type)
	if err != nil {
		return fmt.Errorf("fetching task for dismiss: %w", err)
	}

	// Dismiss the task
	now := time.Now()
	_, err = conn.Exec(ctx,
		`UPDATE tasks SET status = $1, dismissed_at = $2 WHERE id = $3`,
		StatusDismissed, now, taskID,
	)
	if err != nil {
		return fmt.Errorf("dismissing task: %w", err)
	}

	// Record feedback for AI feedback loop (only for AI-extracted tasks)
	if task.SourceSender != "" {
		_, err = conn.Exec(ctx,
			`INSERT INTO task_dismissed_feedback (user_id, tenant_id, task_title, task_description, source_sender, source_subject, task_type)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			ctx.UserID, ctx.TenantID, task.Title, task.Description, task.SourceSender, task.SourceSubject, task.Type,
		)
		if err != nil {
			slog.Warn("task: failed to record dismiss feedback", "error", err)
		}
	}

	return nil
}
