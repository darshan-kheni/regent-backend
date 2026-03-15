package tasks

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// FollowUpService manages delegation follow-up reminders.
type FollowUpService struct {
	pool *pgxpool.Pool
}

// NewFollowUpService creates a new FollowUpService.
func NewFollowUpService(pool *pgxpool.Pool) *FollowUpService {
	return &FollowUpService{pool: pool}
}

// MaxFollowUps is the maximum number of follow-up attempts.
const MaxFollowUps = 3

// FollowUpTone returns the tone for a follow-up based on the count.
func FollowUpTone(count int) string {
	switch {
	case count <= 1:
		return "gentle"
	case count == 2:
		return "direct"
	default:
		return "escalation"
	}
}

// CheckPending finds overdue delegations and handles follow-ups.
// Called by the task_delegation_followup cron job (every 6h).
func (s *FollowUpService) CheckPending(ctx database.TenantContext) error {
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
		`SELECT d.id, d.task_id, d.delegated_to_email, d.delegated_to_name,
		        d.follow_up_count, t.title
		 FROM task_delegations d
		 JOIN tasks t ON t.id = d.task_id
		 WHERE d.user_id = $1
		   AND d.status = 'pending'
		   AND d.follow_up_date IS NOT NULL
		   AND d.follow_up_date <= now()
		   AND d.follow_up_count < $2`,
		ctx.UserID, MaxFollowUps,
	)
	if err != nil {
		return fmt.Errorf("querying pending follow-ups: %w", err)
	}
	defer rows.Close()

	type pending struct {
		delegationID  interface{}
		taskID        interface{}
		email         string
		name          string
		followUpCount int
		taskTitle     string
	}

	var items []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.delegationID, &p.taskID, &p.email, &p.name, &p.followUpCount, &p.taskTitle); err != nil {
			slog.Warn("followup: scan failed", "error", err)
			continue
		}
		items = append(items, p)
	}
	rows.Close()

	now := time.Now()
	for _, p := range items {
		tone := FollowUpTone(p.followUpCount + 1)

		// TODO: Wire to AI email generation (ministral-3:8b ~200 tokens) with tone matching
		slog.Info("followup: would send follow-up",
			"user_id", ctx.UserID,
			"delegatee", p.email,
			"task", p.taskTitle,
			"tone", tone,
			"attempt", p.followUpCount+1,
		)

		// Update delegation record
		nextFollowUp := now.Add(48 * time.Hour) // Default 48h between follow-ups
		_, err := conn.Exec(ctx,
			`UPDATE task_delegations
			 SET follow_up_count = follow_up_count + 1,
			     last_follow_up = $1,
			     follow_up_date = $2
			 WHERE id = $3`,
			now, nextFollowUp, p.delegationID,
		)
		if err != nil {
			slog.Warn("followup: failed to update delegation",
				"delegation_id", p.delegationID,
				"error", fmt.Errorf("updating follow-up: %w", err),
			)
		}

		// If this was the last follow-up, mark as overdue and alert user
		if p.followUpCount+1 >= MaxFollowUps {
			_, err := conn.Exec(ctx,
				`UPDATE task_delegations SET status = 'overdue', follow_up_date = NULL WHERE id = $1`,
				p.delegationID,
			)
			if err != nil {
				slog.Warn("followup: failed to mark overdue", "error", err)
			}

			slog.Warn("followup: max follow-ups reached, alerting user",
				"task", p.taskTitle,
				"delegatee", p.email,
			)
		}
	}

	return nil
}
