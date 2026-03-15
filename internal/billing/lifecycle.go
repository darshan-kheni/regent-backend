package billing

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// StartGracePeriod begins a 7-day grace period for a tenant whose subscription
// was deleted. After the grace period expires, the tenant is downgraded to free.
func StartGracePeriod(ctx context.Context, pool *pgxpool.Pool, customerID string) error {
	tag, err := pool.Exec(ctx, `
		UPDATE tenants
		SET grace_period_ends = NOW() + INTERVAL '7 days',
		    payment_status = 'grace_period',
		    updated_at = NOW()
		WHERE stripe_customer_id = $1
	`, customerID)
	if err != nil {
		return fmt.Errorf("starting grace period for customer %s: %w", customerID, err)
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("billing: no tenant found for grace period",
			"customer_id", customerID,
		)
		return nil
	}

	slog.Info("billing: grace period started (7 days)",
		"customer_id", customerID,
	)
	return nil
}

// CheckGraceExpiry finds all tenants whose grace period has expired and
// downgrades them to the free plan. This should be called periodically
// (e.g., every hour via cron).
func CheckGraceExpiry(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client) error {
	rows, err := pool.Query(ctx, `
		SELECT id FROM tenants
		WHERE grace_period_ends IS NOT NULL
		  AND grace_period_ends < NOW()
		  AND plan != $1
	`, PlanFree)
	if err != nil {
		return fmt.Errorf("querying expired grace periods: %w", err)
	}
	defer rows.Close()

	var expiredCount int
	for rows.Next() {
		var tenantID uuid.UUID
		if err := rows.Scan(&tenantID); err != nil {
			slog.Error("billing: failed to scan expired tenant", "error", err)
			continue
		}

		if err := DowngradeToPlan(ctx, pool, rdb, tenantID, PlanFree); err != nil {
			slog.Error("billing: failed to downgrade expired tenant",
				"tenant_id", tenantID,
				"error", err,
			)
			continue
		}
		expiredCount++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating expired tenants: %w", err)
	}

	if expiredCount > 0 {
		slog.Info("billing: grace periods expired, tenants downgraded",
			"count", expiredCount,
		)
	}
	return nil
}
