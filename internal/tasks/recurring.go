package tasks

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/agnivade/levenshtein"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// RecurringDetector detects if a task title appears 3+ times in 30 days.
type RecurringDetector struct {
	pool *pgxpool.Pool
}

// NewRecurringDetector creates a new RecurringDetector.
func NewRecurringDetector(pool *pgxpool.Pool) *RecurringDetector {
	return &RecurringDetector{pool: pool}
}

// Check looks for similar tasks in the last 30 days and flags as recurring if 3+ found.
func (r *RecurringDetector) Check(ctx database.TenantContext, task *Task) {
	if r.pool == nil {
		return
	}

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		slog.Debug("recurring: failed to acquire connection", "error", err)
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		slog.Debug("recurring: failed to set RLS", "error", err)
		return
	}

	rows, err := conn.Query(ctx,
		`SELECT title, created_at FROM tasks
		 WHERE user_id = $1 AND id != $2
		   AND created_at > now() - interval '30 days'
		 ORDER BY created_at DESC`,
		task.UserID, task.ID,
	)
	if err != nil {
		slog.Debug("recurring: query failed",
			"error", fmt.Errorf("querying similar tasks: %w", err),
		)
		return
	}
	defer rows.Close()

	var similarCount int
	var timestamps []time.Time

	for rows.Next() {
		var title string
		var createdAt time.Time
		if err := rows.Scan(&title, &createdAt); err != nil {
			continue
		}
		distance := levenshtein.ComputeDistance(task.Title, title)
		maxLen := len(task.Title)
		if len(title) > maxLen {
			maxLen = len(title)
		}
		if maxLen > 0 && float64(distance)/float64(maxLen) < 0.20 {
			similarCount++
			timestamps = append(timestamps, createdAt)
		}
	}

	// Need 2+ similar existing tasks (plus this one = 3+ total)
	if similarCount < 2 {
		return
	}

	// Detect interval pattern
	rule := detectRecurrenceRule(timestamps)

	// Update task as recurring
	_, err = conn.Exec(ctx,
		`UPDATE tasks SET type = $1, recurrence_rule = $2, next_recurrence = $3
		 WHERE id = $4`,
		TypeRecurring, rule, nextOccurrence(rule, time.Now()), task.ID,
	)
	if err != nil {
		slog.Warn("recurring: failed to update task",
			"task_id", task.ID,
			"error", fmt.Errorf("updating recurring task: %w", err),
		)
		return
	}

	task.Type = TypeRecurring
	task.RecurrenceRule = rule

	slog.Info("recurring: task flagged as recurring",
		"task_id", task.ID,
		"title", task.Title,
		"rule", rule,
	)
}

// detectRecurrenceRule infers a recurrence pattern from timestamp intervals.
func detectRecurrenceRule(timestamps []time.Time) string {
	if len(timestamps) < 2 {
		return "weekly" // default
	}

	// Calculate average interval between occurrences
	var totalDays float64
	for i := 0; i < len(timestamps)-1; i++ {
		diff := timestamps[i].Sub(timestamps[i+1]).Hours() / 24
		if diff < 0 {
			diff = -diff
		}
		totalDays += diff
	}
	avgDays := totalDays / float64(len(timestamps)-1)

	switch {
	case avgDays <= 1.5:
		return "daily"
	case avgDays <= 10:
		return "weekly"
	case avgDays <= 21:
		return "biweekly"
	default:
		return "monthly"
	}
}

// nextOccurrence computes the next occurrence based on a recurrence rule.
func nextOccurrence(rule string, from time.Time) time.Time {
	switch rule {
	case "daily":
		return from.Add(24 * time.Hour)
	case "weekly":
		return from.Add(7 * 24 * time.Hour)
	case "biweekly":
		return from.Add(14 * 24 * time.Hour)
	case "monthly":
		return from.AddDate(0, 1, 0)
	default:
		return from.Add(7 * 24 * time.Hour)
	}
}
