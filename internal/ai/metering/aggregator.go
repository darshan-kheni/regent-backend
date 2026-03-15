package metering

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Aggregator syncs ai_audit_log to token_usage_daily.
type Aggregator struct {
	pool *pgxpool.Pool
}

// NewAggregator creates a new Aggregator backed by the given connection pool.
func NewAggregator(pool *pgxpool.Pool) *Aggregator {
	return &Aggregator{pool: pool}
}

// RunHourly aggregates recent audit log entries into token_usage_daily.
func (a *Aggregator) RunHourly(ctx context.Context) error {
	start := time.Now()

	_, err := a.pool.Exec(ctx,
		`INSERT INTO token_usage_daily (user_id, tenant_id, date, service, model_used, total_tokens, total_calls, total_latency_ms, cache_hits, premium_calls)
		 SELECT
		   user_id, tenant_id, created_at::date AS date, task_type AS service, model_used,
		   SUM(tokens_in + tokens_out), COUNT(*), SUM(latency_ms),
		   COUNT(*) FILTER (WHERE cache_hit), COUNT(*) FILTER (WHERE model_used LIKE '%120b%')
		 FROM ai_audit_log
		 WHERE created_at > now() - interval '2 hours'
		 GROUP BY user_id, tenant_id, created_at::date, task_type, model_used
		 ON CONFLICT (user_id, date, service, model_used) DO UPDATE SET
		   total_tokens = token_usage_daily.total_tokens + EXCLUDED.total_tokens,
		   total_calls = token_usage_daily.total_calls + EXCLUDED.total_calls,
		   total_latency_ms = token_usage_daily.total_latency_ms + EXCLUDED.total_latency_ms,
		   cache_hits = token_usage_daily.cache_hits + EXCLUDED.cache_hits,
		   premium_calls = token_usage_daily.premium_calls + EXCLUDED.premium_calls`,
	)
	if err != nil {
		return fmt.Errorf("hourly aggregation: %w", err)
	}

	slog.Info("hourly token aggregation complete", "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// RefreshMaterializedView refreshes the token_usage_summary materialized view.
// Should be called every 6 hours.
func (a *Aggregator) RefreshMaterializedView(ctx context.Context) error {
	start := time.Now()

	_, err := a.pool.Exec(ctx, `REFRESH MATERIALIZED VIEW CONCURRENTLY token_usage_summary`)
	if err != nil {
		return fmt.Errorf("refreshing materialized view: %w", err)
	}

	slog.Info("materialized view refreshed", "duration_ms", time.Since(start).Milliseconds())
	return nil
}
