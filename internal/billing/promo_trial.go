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

// provisionTrial upgrades the tenant to the promo plan for the trial period.
func (s *PromoService) provisionTrial(ctx database.TenantContext, userID uuid.UUID, promo PromoCode) error {
	if promo.TrialDays == nil {
		return fmt.Errorf("trial promo missing trial_days")
	}

	// Update tenant plan directly for trial (no Stripe subscription needed)
	_, err := s.pool.Exec(ctx,
		`UPDATE tenants
		 SET plan = $1,
		     plan_started_at = now(),
		     plan_renews_at = now() + ($2 || ' days')::interval
		 WHERE id = $3`,
		promo.Plan, fmt.Sprintf("%d", *promo.TrialDays), ctx.TenantID,
	)
	if err != nil {
		return fmt.Errorf("updating tenant plan for trial: %w", err)
	}

	// Invalidate cached plan
	if s.rdb != nil {
		if cacheErr := InvalidatePlanCache(ctx, ctx.TenantID, s.rdb); cacheErr != nil {
			slog.Warn("billing: failed to invalidate plan cache after trial provision",
				"tenant_id", ctx.TenantID,
				"error", cacheErr,
			)
		}
	}

	slog.Info("billing: provisioned trial",
		"tenant_id", ctx.TenantID,
		"user_id", userID,
		"plan", promo.Plan,
		"trial_days", *promo.TrialDays,
		"code", promo.Code,
	)

	return nil
}

// CheckTrialExpiry finds expired trials and downgrades tenants to the free plan.
// This should be called periodically (e.g., every hour) from a cron job.
func CheckTrialExpiry(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client) error {
	rows, err := pool.Query(ctx,
		`SELECT pr.tenant_id, pr.user_id, pr.applied_plan, pr.code_id
		 FROM promo_redemptions pr
		 JOIN promo_codes pc ON pc.id = pr.code_id
		 WHERE pc.type = 'trial'
		   AND pr.trial_end_date IS NOT NULL
		   AND pr.trial_end_date < now()
		   AND EXISTS (
		       SELECT 1 FROM tenants t
		       WHERE t.id = pr.tenant_id AND t.plan = pr.applied_plan
		   )`)
	if err != nil {
		return fmt.Errorf("querying expired trials: %w", err)
	}
	defer rows.Close()

	type expiredTrial struct {
		TenantID uuid.UUID
		UserID   uuid.UUID
		Plan     string
		CodeID   uuid.UUID
	}

	var expired []expiredTrial
	for rows.Next() {
		var et expiredTrial
		if err := rows.Scan(&et.TenantID, &et.UserID, &et.Plan, &et.CodeID); err != nil {
			slog.Error("billing: failed to scan expired trial", "error", err)
			continue
		}
		expired = append(expired, et)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating expired trials: %w", err)
	}

	for _, et := range expired {
		slog.Info("billing: downgrading expired trial",
			"tenant_id", et.TenantID,
			"plan", et.Plan,
		)

		_, err := pool.Exec(ctx,
			`UPDATE tenants
			 SET plan = $1, plan_started_at = now(), plan_renews_at = NULL
			 WHERE id = $2 AND plan = $3`,
			PlanFree, et.TenantID, et.Plan,
		)
		if err != nil {
			slog.Error("billing: failed to downgrade expired trial",
				"tenant_id", et.TenantID,
				"error", err,
			)
			continue
		}

		// Invalidate cached plan
		if rdb != nil {
			if cacheErr := InvalidatePlanCache(ctx, et.TenantID, rdb); cacheErr != nil {
				slog.Warn("billing: failed to invalidate plan cache after trial expiry",
					"tenant_id", et.TenantID,
					"error", cacheErr,
				)
			}
		}

		slog.Info("billing: successfully downgraded expired trial to free",
			"tenant_id", et.TenantID,
			"previous_plan", et.Plan,
		)
	}

	if len(expired) > 0 {
		slog.Info("billing: trial expiry check complete", "downgraded", len(expired))
	}

	return nil
}
