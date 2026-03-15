package billing

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/database"
)

// ProvisionPlan sets a tenant's plan after a successful checkout or webhook.
// Updates the tenant record with the plan, Stripe IDs, and resets payment state.
func ProvisionPlan(tc database.TenantContext, pool *pgxpool.Pool, rdb *redis.Client, planName, stripeCustomerID, stripeSubscriptionID string) error {
	conn, err := pool.Acquire(tc)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	tag, err := conn.Exec(tc, `
		UPDATE tenants
		SET plan = $1,
		    stripe_customer_id = $2,
		    stripe_subscription_id = $3,
		    plan_started_at = NOW(),
		    payment_status = 'active',
		    failure_count = 0,
		    first_failure_at = NULL,
		    grace_period_ends = NULL,
		    updated_at = NOW()
		WHERE id = $4
	`, planName, stripeCustomerID, stripeSubscriptionID, tc.TenantID)
	if err != nil {
		return fmt.Errorf("updating tenant plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant %s not found", tc.TenantID)
	}

	if err := InvalidatePlanCache(tc, tc.TenantID, rdb); err != nil {
		slog.Warn("billing: failed to invalidate cache after provision",
			"tenant_id", tc.TenantID,
			"error", err,
		)
	}

	slog.Info("billing: plan provisioned",
		"tenant_id", tc.TenantID,
		"plan", planName,
		"stripe_customer_id", stripeCustomerID,
	)
	return nil
}

// AdjustLimits updates a tenant's plan when the subscription changes (upgrade/downgrade).
func AdjustLimits(tc database.TenantContext, pool *pgxpool.Pool, rdb *redis.Client, newPlan string) error {
	conn, err := pool.Acquire(tc)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	tag, err := conn.Exec(tc, `
		UPDATE tenants
		SET plan = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, newPlan, tc.TenantID)
	if err != nil {
		return fmt.Errorf("adjusting plan limits: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant %s not found", tc.TenantID)
	}

	if err := InvalidatePlanCache(tc, tc.TenantID, rdb); err != nil {
		slog.Warn("billing: failed to invalidate cache after adjust",
			"tenant_id", tc.TenantID,
			"error", err,
		)
	}

	slog.Info("billing: plan adjusted",
		"tenant_id", tc.TenantID,
		"new_plan", newPlan,
	)
	return nil
}

// DowngradeToPlan downgrades a tenant to a specified plan. If the target is
// the free plan, the Stripe subscription ID is cleared.
func DowngradeToPlan(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, tenantID uuid.UUID, plan string) error {
	// Build query based on target plan
	var err error
	if plan == PlanFree {
		_, err = pool.Exec(ctx, `
			UPDATE tenants
			SET plan = $1,
			    stripe_subscription_id = NULL,
			    grace_period_ends = NULL,
			    failure_count = 0,
			    first_failure_at = NULL,
			    payment_status = 'none',
			    updated_at = NOW()
			WHERE id = $2
		`, plan, tenantID)
	} else {
		_, err = pool.Exec(ctx, `
			UPDATE tenants
			SET plan = $1,
			    updated_at = NOW()
			WHERE id = $2
		`, plan, tenantID)
	}
	if err != nil {
		return fmt.Errorf("downgrading tenant to %s: %w", plan, err)
	}

	if cacheErr := InvalidatePlanCache(ctx, tenantID, rdb); cacheErr != nil {
		slog.Warn("billing: failed to invalidate cache after downgrade",
			"tenant_id", tenantID,
			"error", cacheErr,
		)
	}

	slog.Info("billing: tenant downgraded",
		"tenant_id", tenantID,
		"plan", plan,
	)
	return nil
}
