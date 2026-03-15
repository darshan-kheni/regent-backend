package billing

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/briefings"
)

// HandlePaymentFailure implements the 3-step dunning process for failed payments.
//
// Step 1 (failure_count=1): Notify "Payment failed, please update card"
// Step 2 (failure_count=2): Notify "Final warning — service will be suspended"
// Step 3 (failure_count>=3, 7+ days since first failure): Suspend account
func HandlePaymentFailure(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, customerID string) error {
	// Increment failure count and get current state
	var tenantID uuid.UUID
	var userID uuid.UUID
	var failureCount int
	var daysSinceFirst int

	err := pool.QueryRow(ctx, `
		UPDATE tenants
		SET failure_count = failure_count + 1,
		    payment_status = CASE
		        WHEN failure_count + 1 >= 3 THEN 'suspended'
		        ELSE 'failing'
		    END,
		    updated_at = NOW()
		WHERE stripe_customer_id = $1
		RETURNING id,
			failure_count,
			COALESCE(EXTRACT(DAY FROM NOW() - first_failure_at)::int, 0)
	`, customerID).Scan(&tenantID, &failureCount, &daysSinceFirst)
	if err != nil {
		return fmt.Errorf("updating failure count for customer %s: %w", customerID, err)
	}

	// Look up user for notifications
	err = pool.QueryRow(ctx, `
		SELECT id FROM users WHERE tenant_id = $1 LIMIT 1
	`, tenantID).Scan(&userID)
	if err != nil {
		slog.Warn("billing: no user found for tenant during dunning",
			"tenant_id", tenantID,
			"error", err,
		)
		userID = uuid.Nil
	}

	// Set first_failure_at on first failure
	if failureCount == 1 {
		_, err = pool.Exec(ctx, `
			UPDATE tenants SET first_failure_at = NOW() WHERE id = $1 AND first_failure_at IS NULL
		`, tenantID)
		if err != nil {
			slog.Error("billing: failed to set first_failure_at", "tenant_id", tenantID, "error", err)
		}
	}

	slog.Info("billing: payment failure recorded",
		"tenant_id", tenantID,
		"customer_id", customerID,
		"failure_count", failureCount,
		"days_since_first", daysSinceFirst,
	)

	switch {
	case failureCount >= 3 && daysSinceFirst >= 7:
		// Step 3: Suspend account
		if err := suspendAccount(ctx, pool, rdb, tenantID); err != nil {
			return fmt.Errorf("suspending account: %w", err)
		}
		_ = briefings.PublishNotificationEvent(ctx, rdb, briefings.NotificationEvent{
			UserID:   userID.String(),
			TenantID: tenantID.String(),
			Priority: 100,
			Category: "billing",
			Subject:  "Account Suspended — Payment Failed",
			Summary:  "Your account has been suspended due to repeated payment failures. Please update your payment method to restore access.",
			Channels: []string{"email", "push"},
		})

	case failureCount == 2:
		// Step 2: Final warning
		_ = briefings.PublishNotificationEvent(ctx, rdb, briefings.NotificationEvent{
			UserID:   userID.String(),
			TenantID: tenantID.String(),
			Priority: 90,
			Category: "billing",
			Subject:  "Final Warning — Payment Still Failing",
			Summary:  "Your payment has failed a second time. Please update your payment method within 5 days to avoid service suspension.",
			Channels: []string{"email", "push"},
		})

	case failureCount == 1:
		// Step 1: Initial notification
		_ = briefings.PublishNotificationEvent(ctx, rdb, briefings.NotificationEvent{
			UserID:   userID.String(),
			TenantID: tenantID.String(),
			Priority: 80,
			Category: "billing",
			Subject:  "Payment Failed — Please Update Your Card",
			Summary:  "We were unable to process your subscription payment. Please update your payment method to continue uninterrupted service.",
			Channels: []string{"email", "push"},
		})
	}

	return nil
}

// suspendAccount marks a tenant as suspended and downgrades to free.
func suspendAccount(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, tenantID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE tenants
		SET payment_status = 'suspended',
		    plan = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, PlanFree, tenantID)
	if err != nil {
		return fmt.Errorf("suspending tenant %s: %w", tenantID, err)
	}

	if err := InvalidatePlanCache(ctx, tenantID, rdb); err != nil {
		slog.Warn("billing: failed to invalidate cache after suspend",
			"tenant_id", tenantID,
			"error", err,
		)
	}

	slog.Warn("billing: account suspended due to payment failures",
		"tenant_id", tenantID,
	)
	return nil
}
