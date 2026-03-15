package billing

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// CheckUsageLimit queries current monthly usage vs plan limit for a given field.
// Returns allowed=true if the limit is 0 (unlimited) or usage is below the limit.
// Valid fields: emails_processed, ai_calls, tokens_consumed, storage_bytes, notifications_sent.
func CheckUsageLimit(ctx database.TenantContext, pool *pgxpool.Pool, field string) (allowed bool, current int64, limit int64, err error) {
	if !allowedUsageFields[field] {
		return false, 0, 0, fmt.Errorf("invalid usage field: %s", field)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, 0, 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return false, 0, 0, fmt.Errorf("setting RLS context: %w", err)
	}

	// Get current month total for the field.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(%s), 0)
		FROM tenant_usage
		WHERE tenant_id = $1 AND period = 'daily'
		  AND period_date >= $2 AND period_date <= CURRENT_DATE
	`, field)

	err = conn.QueryRow(ctx, query, ctx.TenantID, monthStart).Scan(&current)
	if err != nil {
		return false, 0, 0, fmt.Errorf("querying current usage for %s: %w", field, err)
	}

	// Get plan and its limits.
	var plan string
	err = conn.QueryRow(ctx,
		"SELECT COALESCE(plan, 'free') FROM tenants WHERE id = $1",
		ctx.TenantID,
	).Scan(&plan)
	if err != nil {
		return false, 0, 0, fmt.Errorf("querying tenant plan: %w", err)
	}

	planDef, ok := GetPlanByName(plan)
	if !ok {
		planDef, _ = GetPlanByName(PlanFree)
	}

	// Map field to plan limit.
	switch field {
	case "tokens_consumed":
		limit = planDef.Limits.DailyTokens * 30 // Monthly approximation.
	case "emails_processed":
		limit = planDef.Limits.EmailsPerMonth
	default:
		// Fields without explicit plan limits are unlimited.
		limit = 0
	}

	// 0 means unlimited.
	if limit == 0 {
		return true, current, limit, nil
	}

	return current < limit, current, limit, nil
}

// CheckDailyTokenLimit checks if the tenant has exceeded their daily token limit.
// Returns allowed=true if the limit is 0 (unlimited) or usage is below the limit.
func CheckDailyTokenLimit(ctx database.TenantContext, pool *pgxpool.Pool) (allowed bool, current int64, limit int64, err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, 0, 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if rlsErr := database.SetRLSContext(ctx, conn); rlsErr != nil {
		return false, 0, 0, fmt.Errorf("setting RLS context: %w", rlsErr)
	}

	// Get today's token usage.
	err = conn.QueryRow(ctx, `
		SELECT COALESCE(tokens_consumed, 0)
		FROM tenant_usage
		WHERE tenant_id = $1 AND period = 'daily' AND period_date = CURRENT_DATE
	`, ctx.TenantID).Scan(&current)
	if err != nil {
		// No row means zero usage.
		current = 0
	}

	// Get plan.
	var plan string
	err = conn.QueryRow(ctx,
		"SELECT COALESCE(plan, 'free') FROM tenants WHERE id = $1",
		ctx.TenantID,
	).Scan(&plan)
	if err != nil {
		return false, 0, 0, fmt.Errorf("querying tenant plan: %w", err)
	}

	planDef, ok := GetPlanByName(plan)
	if !ok {
		planDef, _ = GetPlanByName(PlanFree)
	}

	limit = planDef.Limits.DailyTokens

	// 0 means unlimited.
	if limit == 0 {
		return true, current, limit, nil
	}

	return current < limit, current, limit, nil
}
