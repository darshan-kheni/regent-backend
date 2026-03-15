package tasks

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// DelegationService manages task delegation to external contacts.
type DelegationService struct {
	pool *pgxpool.Pool
}

// NewDelegationService creates a new DelegationService.
func NewDelegationService(pool *pgxpool.Pool) *DelegationService {
	return &DelegationService{pool: pool}
}

// DelegateInput is the input for delegating a task.
type DelegateInput struct {
	TaskID         uuid.UUID `json:"task_id"`
	DelegateeEmail string    `json:"delegatee_email"`
	DelegateeName  string    `json:"delegatee_name"`
	FollowUpDate   time.Time `json:"follow_up_date"`
}

// Delegate creates a delegation record and updates the task status.
func (s *DelegationService) Delegate(ctx database.TenantContext, input DelegateInput) (*TaskDelegation, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS: %w", err)
	}

	// Create delegation record
	var delegation TaskDelegation
	err = conn.QueryRow(ctx,
		`INSERT INTO task_delegations (task_id, user_id, tenant_id, delegated_to_email, delegated_to_name, follow_up_date)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, task_id, user_id, tenant_id, delegated_to_email, delegated_to_name, status, follow_up_date, follow_up_count, created_at`,
		input.TaskID, ctx.UserID, ctx.TenantID, input.DelegateeEmail, input.DelegateeName, input.FollowUpDate,
	).Scan(
		&delegation.ID, &delegation.TaskID, &delegation.UserID, &delegation.TenantID,
		&delegation.DelegatedToEmail, &delegation.DelegatedToName,
		&delegation.Status, &delegation.FollowUpDate, &delegation.FollowUpCount, &delegation.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting delegation: %w", err)
	}

	// Update task status to waiting and set delegation fields
	now := time.Now()
	_, err = conn.Exec(ctx,
		`UPDATE tasks SET status = $1, delegated_to = $2, delegated_at = $3 WHERE id = $4`,
		StatusWaiting, input.DelegateeEmail, now, input.TaskID,
	)
	if err != nil {
		return nil, fmt.Errorf("updating task status: %w", err)
	}

	// Create follow-up reminder
	_, err = conn.Exec(ctx,
		`INSERT INTO task_reminders (task_id, user_id, tenant_id, reminder_type, scheduled_at)
		 VALUES ($1, $2, $3, 'follow_up', $4)
		 ON CONFLICT (task_id, reminder_type) DO UPDATE SET scheduled_at = EXCLUDED.scheduled_at, sent_at = NULL`,
		input.TaskID, ctx.UserID, ctx.TenantID, input.FollowUpDate,
	)
	if err != nil {
		slog.Warn("delegation: failed to create follow-up reminder", "error", err)
	}

	slog.Info("delegation: task delegated",
		"task_id", input.TaskID,
		"delegatee", input.DelegateeEmail,
		"follow_up", input.FollowUpDate,
	)

	return &delegation, nil
}

// GetDelegations returns all delegations for a task.
func (s *DelegationService) GetDelegations(ctx database.TenantContext, taskID uuid.UUID) ([]TaskDelegation, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, task_id, user_id, tenant_id, delegated_to_email, delegated_to_name,
		        delegation_email_id, status, follow_up_date, follow_up_count, last_follow_up,
		        completed_at, created_at
		 FROM task_delegations
		 WHERE task_id = $1 AND user_id = $2
		 ORDER BY created_at DESC`,
		taskID, ctx.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying delegations: %w", err)
	}
	defer rows.Close()

	var delegations []TaskDelegation
	for rows.Next() {
		var d TaskDelegation
		if err := rows.Scan(
			&d.ID, &d.TaskID, &d.UserID, &d.TenantID,
			&d.DelegatedToEmail, &d.DelegatedToName,
			&d.DelegationEmailID, &d.Status,
			&d.FollowUpDate, &d.FollowUpCount, &d.LastFollowUp,
			&d.CompletedAt, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning delegation: %w", err)
		}
		delegations = append(delegations, d)
	}

	return delegations, nil
}
