package ai

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReprocessSkipped re-enqueues emails that were skipped due to plan limitations.
// Called when a user upgrades their plan.
func ReprocessSkipped(ctx context.Context, pool *pgxpool.Pool, enqueuer Enqueuer, userID uuid.UUID, newPlan string) (int, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquiring connection for reprocess: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT email_id, user_id, tenant_id
		 FROM email_ai_status
		 WHERE user_id = $1 AND stage = 'skipped'`,
		userID,
	)
	if err != nil {
		return 0, fmt.Errorf("querying skipped emails: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var emailID, uid, tenantID uuid.UUID
		if err := rows.Scan(&emailID, &uid, &tenantID); err != nil {
			slog.Error("reprocess: failed to scan row", "error", err)
			continue
		}

		if err := enqueuer.EnqueueEmail(ctx, emailID, uid, tenantID, newPlan); err != nil {
			slog.Error("reprocess: failed to re-enqueue",
				"email_id", emailID,
				"error", err,
			)
			continue
		}
		count++
	}

	if count > 0 {
		slog.Info("reprocess: re-enqueued skipped emails",
			"user_id", userID,
			"new_plan", newPlan,
			"count", count,
		)
	}
	return count, rows.Err()
}
