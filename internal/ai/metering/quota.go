package metering

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// PlanLimits defines token quotas per plan.
type PlanLimits struct {
	DailyTokens    int64 // 0 = unlimited
	PremiumMonthly int   // 0 = unlimited (for estate), or 0 = none (for free)
}

// PlanQuotas maps plan names to their token limits.
var PlanQuotas = map[string]PlanLimits{
	"free":    {DailyTokens: 50_000, PremiumMonthly: 0},
	"attache": {DailyTokens: 500_000, PremiumMonthly: 20},
	"privy_council": {DailyTokens: 2_000_000, PremiumMonthly: 200},
	"estate":  {DailyTokens: 0, PremiumMonthly: 0}, // 0 = unlimited
}

// QuotaChecker enforces real-time token quotas via Redis.
type QuotaChecker struct {
	rdb *redis.Client
}

// NewQuotaChecker creates a new QuotaChecker backed by the given Redis client.
func NewQuotaChecker(rdb *redis.Client) *QuotaChecker {
	return &QuotaChecker{rdb: rdb}
}

// CheckAndIncrement checks quota before an AI call and increments usage.
// Returns: allowed (can proceed), usagePct (0-100+), error.
// Fails open on Redis errors (returns allowed=true).
func (qc *QuotaChecker) CheckAndIncrement(ctx context.Context, userID uuid.UUID, plan string, tokensToAdd int, isPremium bool) (allowed bool, usagePct float64, err error) {
	limits, ok := PlanQuotas[plan]
	if !ok {
		limits = PlanQuotas["free"]
	}

	// Estate = unlimited
	if limits.DailyTokens == 0 && plan == "estate" {
		return true, 0, nil
	}

	// Check daily token quota
	dateKey := fmt.Sprintf("tokens:%s:%s", userID, time.Now().Format("2006-01-02"))

	currentTokens, err := qc.rdb.Get(ctx, dateKey).Int64()
	if err != nil && err != redis.Nil {
		slog.Warn("redis quota check failed, allowing (fail-open)", "error", err)
		return true, 0, nil // fail-open
	}

	usagePct = float64(currentTokens) / float64(limits.DailyTokens) * 100

	if limits.DailyTokens > 0 && currentTokens >= limits.DailyTokens {
		return false, usagePct, nil
	}

	// Increment
	pipe := qc.rdb.Pipeline()
	pipe.IncrBy(ctx, dateKey, int64(tokensToAdd))
	pipe.Expire(ctx, dateKey, 86400*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("redis quota increment failed", "error", err)
		// fail-open: already checked, proceed anyway
	}

	// Check premium quota if applicable
	if isPremium {
		monthKey := fmt.Sprintf("premium:%s:%s", userID, time.Now().Format("2006-01"))
		premiumCount, _ := qc.rdb.Get(ctx, monthKey).Int()

		if limits.PremiumMonthly > 0 && premiumCount >= limits.PremiumMonthly {
			return false, usagePct, nil
		}

		premPipe := qc.rdb.Pipeline()
		premPipe.Incr(ctx, monthKey)
		premPipe.Expire(ctx, monthKey, 31*24*time.Hour)
		premPipe.Exec(ctx) //nolint:errcheck // best-effort increment
	}

	// Recalculate after increment
	newTokens := currentTokens + int64(tokensToAdd)
	usagePct = float64(newTokens) / float64(limits.DailyTokens) * 100

	return true, usagePct, nil
}

// GetUsage returns current daily token usage for a user.
func (qc *QuotaChecker) GetUsage(ctx context.Context, userID uuid.UUID) (int64, error) {
	dateKey := fmt.Sprintf("tokens:%s:%s", userID, time.Now().Format("2006-01-02"))
	val, err := qc.rdb.Get(ctx, dateKey).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}
