package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	// planCacheTTL is how long a plan is cached in Redis before re-query.
	planCacheTTL = 5 * time.Minute

	// planCachePrefix is the Redis key prefix for cached plans.
	planCachePrefix = "plan:"
)

// planCacheKey returns the Redis key for a tenant's cached plan.
func planCacheKey(tenantID uuid.UUID) string {
	return planCachePrefix + tenantID.String()
}

// GetCachedPlan returns the plan name for a tenant, using Redis as a cache
// with DB fallback. Fails open: if Redis is unavailable, queries DB directly.
func GetCachedPlan(ctx context.Context, tenantID uuid.UUID, rdb *redis.Client, pool *pgxpool.Pool) (string, error) {
	// Try Redis cache first
	if rdb != nil {
		plan, err := rdb.Get(ctx, planCacheKey(tenantID)).Result()
		if err == nil && plan != "" {
			return plan, nil
		}
		if err != nil && err != redis.Nil {
			slog.Warn("billing: Redis cache read failed, falling back to DB",
				"tenant_id", tenantID,
				"error", err,
			)
		}
	}

	// Cache miss or Redis unavailable — query DB
	plan, err := queryTenantPlan(ctx, tenantID, pool)
	if err != nil {
		return "", err
	}

	// Populate cache (best-effort)
	if rdb != nil {
		if cacheErr := rdb.Set(ctx, planCacheKey(tenantID), plan, planCacheTTL).Err(); cacheErr != nil {
			slog.Warn("billing: failed to cache plan in Redis",
				"tenant_id", tenantID,
				"error", cacheErr,
			)
		}
	}

	return plan, nil
}

// InvalidatePlanCache removes the cached plan for a tenant, forcing the next
// lookup to query the database.
func InvalidatePlanCache(ctx context.Context, tenantID uuid.UUID, rdb *redis.Client) error {
	if rdb == nil {
		return nil
	}
	if err := rdb.Del(ctx, planCacheKey(tenantID)).Err(); err != nil {
		slog.Warn("billing: failed to invalidate plan cache",
			"tenant_id", tenantID,
			"error", err,
		)
		return fmt.Errorf("invalidating plan cache: %w", err)
	}
	return nil
}

// queryTenantPlan fetches the plan directly from the tenants table.
// This does NOT use RLS because it is called from admin/webhook contexts.
func queryTenantPlan(ctx context.Context, tenantID uuid.UUID, pool *pgxpool.Pool) (string, error) {
	var plan string
	err := pool.QueryRow(ctx,
		"SELECT plan FROM tenants WHERE id = $1",
		tenantID,
	).Scan(&plan)
	if err != nil {
		return "", fmt.Errorf("querying tenant plan: %w", err)
	}
	return plan, nil
}
