package tasks

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// RecurrenceProcessor handles automatic creation of recurring task instances.
// Called by the task_recurrence cron job (1h interval).
type RecurrenceProcessor struct {
	pool *pgxpool.Pool
}

// NewRecurrenceProcessor creates a new RecurrenceProcessor.
func NewRecurrenceProcessor(pool *pgxpool.Pool) *RecurrenceProcessor {
	return &RecurrenceProcessor{pool: pool}
}

// ProcessRecurrence finds recurring tasks whose next_recurrence is due and creates new instances.
func (p *RecurrenceProcessor) ProcessRecurrence(ctx database.TenantContext) error {
	if p.pool == nil {
		return nil
	}

	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, title, description, type, priority,
		        recurrence_rule, next_recurrence
		 FROM tasks
		 WHERE user_id = $1
		   AND recurrence_rule IS NOT NULL
		   AND next_recurrence IS NOT NULL
		   AND next_recurrence <= now()
		   AND status != 'dismissed'`,
		ctx.UserID,
	)
	if err != nil {
		return fmt.Errorf("querying due recurring tasks: %w", err)
	}
	defer rows.Close()

	type recurringTask struct {
		task           Task
		recurrenceRule string
		nextRecurrence time.Time
	}

	var dueTasks []recurringTask
	for rows.Next() {
		var rt recurringTask
		if err := rows.Scan(
			&rt.task.ID, &rt.task.UserID, &rt.task.TenantID,
			&rt.task.Title, &rt.task.Description, &rt.task.Type, &rt.task.Priority,
			&rt.recurrenceRule, &rt.nextRecurrence,
		); err != nil {
			slog.Warn("recurrence: failed to scan row", "error", err)
			continue
		}
		dueTasks = append(dueTasks, rt)
	}
	rows.Close()

	for _, rt := range dueTasks {
		// Create new task instance
		newDeadline := nextOccurrence(rt.recurrenceRule, rt.nextRecurrence)
		var newTaskID interface{}
		err := conn.QueryRow(ctx,
			`INSERT INTO tasks (user_id, tenant_id, title, description, type, status, priority, deadline, recurrence_rule, next_recurrence)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 RETURNING id`,
			rt.task.UserID, rt.task.TenantID,
			rt.task.Title, rt.task.Description, TypeRecurring, StatusToDo, rt.task.Priority,
			newDeadline, rt.recurrenceRule, nextOccurrence(rt.recurrenceRule, newDeadline),
		).Scan(&newTaskID)
		if err != nil {
			slog.Warn("recurrence: failed to create new instance",
				"parent_task_id", rt.task.ID,
				"error", fmt.Errorf("inserting recurring task: %w", err),
			)
			continue
		}

		// Update original task's next_recurrence
		nextNext := nextOccurrence(rt.recurrenceRule, rt.nextRecurrence)
		_, err = conn.Exec(ctx,
			`UPDATE tasks SET next_recurrence = $1 WHERE id = $2`,
			nextOccurrence(rt.recurrenceRule, nextNext), rt.task.ID,
		)
		if err != nil {
			slog.Warn("recurrence: failed to update next_recurrence",
				"task_id", rt.task.ID,
				"error", err,
			)
		}

		slog.Info("recurrence: created recurring task instance",
			"parent_task_id", rt.task.ID,
			"new_task_id", newTaskID,
			"title", rt.task.Title,
			"rule", rt.recurrenceRule,
		)
	}

	return nil
}
