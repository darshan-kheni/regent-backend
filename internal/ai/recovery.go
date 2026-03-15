package ai

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RecoverIncompleteJobs re-enqueues any emails that were in-progress when
// the server crashed. Called during server boot.
func RecoverIncompleteJobs(ctx context.Context, pool *pgxpool.Pool, enqueuer Enqueuer) (int, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquiring connection for recovery: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT email_id, user_id, tenant_id, plan
		 FROM email_ai_status
		 WHERE stage NOT IN ('complete', 'error', 'skipped')`,
	)
	if err != nil {
		return 0, fmt.Errorf("querying incomplete jobs: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var emailID, userID, tenantID uuid.UUID
		var plan string
		if err := rows.Scan(&emailID, &userID, &tenantID, &plan); err != nil {
			slog.Error("recovery: failed to scan row", "error", err)
			continue
		}

		if err := enqueuer.EnqueueEmail(ctx, emailID, userID, tenantID, plan); err != nil {
			slog.Error("recovery: failed to re-enqueue",
				"email_id", emailID,
				"error", err,
			)
			continue
		}
		count++
	}

	if count > 0 {
		slog.Info("recovery: re-enqueued incomplete jobs", "count", count)
	}
	return count, rows.Err()
}
