package billing

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/database"
)

// allowedUsageFields are the only fields that can be incremented in tenant_usage.
var allowedUsageFields = map[string]bool{
	"emails_processed":  true,
	"ai_calls":          true,
	"tokens_consumed":   true,
	"storage_bytes":     true,
	"notifications_sent": true,
}

// UsageData represents current usage vs plan limits.
type UsageData struct {
	DailyTokens   int64 `json:"daily_tokens"`
	DailyLimit    int64 `json:"daily_limit"`
	MonthlyTokens int64 `json:"monthly_tokens"`
	MonthlyLimit  int64 `json:"monthly_limit"`
	EmailsUsed    int64 `json:"emails_used"`
	EmailsLimit   int64 `json:"emails_limit"`
	AICallsToday  int64 `json:"ai_calls_today"`
	PeriodStart   string `json:"period_start"`
	PeriodEnd     string `json:"period_end"`
}

// ServiceBreakdown represents per-service AI usage stats.
type ServiceBreakdown struct {
	ServiceName  string  `json:"service_name"`
	Model        string  `json:"model"`
	Tokens       int64   `json:"tokens"`
	Calls        int     `json:"calls"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	UsagePercent float64 `json:"usage_percent"`
}

// UsageService handles usage metering operations.
type UsageService struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// NewUsageService creates a new UsageService.
func NewUsageService(pool *pgxpool.Pool, rdb *redis.Client) *UsageService {
	return &UsageService{
		pool: pool,
		rdb:  rdb,
	}
}

// Pool returns the underlying connection pool.
func (s *UsageService) Pool() *pgxpool.Pool {
	return s.pool
}

// IncrementUsage atomically increments a usage field for the current day.
// Uses INSERT ON CONFLICT DO UPDATE for atomic upsert.
// Valid fields: emails_processed, ai_calls, tokens_consumed, storage_bytes, notifications_sent.
func (s *UsageService) IncrementUsage(ctx database.TenantContext, field string, amount int64) error {
	if !allowedUsageFields[field] {
		return fmt.Errorf("invalid usage field: %s", field)
	}
	if amount <= 0 {
		return fmt.Errorf("amount must be positive, got %d", amount)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Use dynamic SQL with validated field name (safe because we checked allowedUsageFields).
	query := fmt.Sprintf(`
		INSERT INTO tenant_usage (tenant_id, period, period_date, %s)
		VALUES ($1, 'daily', CURRENT_DATE, $2)
		ON CONFLICT (tenant_id, period, period_date) DO UPDATE
		SET %s = tenant_usage.%s + $2
	`, field, field, field)

	_, err = conn.Exec(ctx, query, ctx.TenantID, amount)
	if err != nil {
		return fmt.Errorf("incrementing usage %s: %w", field, err)
	}

	slog.Debug("usage incremented",
		"tenant_id", ctx.TenantID,
		"field", field,
		"amount", amount,
	)

	return nil
}

// GetUsage returns usage data for the given period ("daily" or "monthly").
func (s *UsageService) GetUsage(ctx database.TenantContext, period string) (*UsageData, error) {
	if period != "daily" && period != "monthly" {
		period = "daily"
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	data := &UsageData{}
	now := time.Now().UTC()

	// Get daily token usage.
	err = conn.QueryRow(ctx, `
		SELECT COALESCE(tokens_consumed, 0), COALESCE(ai_calls, 0)
		FROM tenant_usage
		WHERE tenant_id = $1 AND period = 'daily' AND period_date = CURRENT_DATE
	`, ctx.TenantID).Scan(&data.DailyTokens, &data.AICallsToday)
	if err != nil {
		// No row means zero usage.
		data.DailyTokens = 0
		data.AICallsToday = 0
	}

	// Get monthly totals (sum all daily rows for current month).
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	err = conn.QueryRow(ctx, `
		SELECT COALESCE(SUM(tokens_consumed), 0), COALESCE(SUM(emails_processed), 0)
		FROM tenant_usage
		WHERE tenant_id = $1 AND period = 'daily'
		  AND period_date >= $2 AND period_date <= CURRENT_DATE
	`, ctx.TenantID, monthStart).Scan(&data.MonthlyTokens, &data.EmailsUsed)
	if err != nil {
		data.MonthlyTokens = 0
		data.EmailsUsed = 0
	}

	// Get plan limits.
	plan, err := GetCachedPlan(ctx, ctx.TenantID, s.rdb, s.pool)
	if err != nil {
		slog.Warn("failed to get cached plan, using free limits",
			"tenant_id", ctx.TenantID,
			"error", err,
		)
		plan = PlanFree
	}

	planDef, ok := GetPlanByName(plan)
	if !ok {
		planDef, _ = GetPlanByName(PlanFree)
	}

	data.DailyLimit = planDef.Limits.DailyTokens
	data.MonthlyLimit = planDef.Limits.DailyTokens * 30 // Approximate monthly limit.
	data.EmailsLimit = planDef.Limits.EmailsPerMonth
	data.PeriodStart = monthStart.Format("2006-01-02")
	data.PeriodEnd = now.Format("2006-01-02")

	return data, nil
}

// GetUsageBreakdown returns per-service AI usage stats from ai_audit_log.
func (s *UsageService) GetUsageBreakdown(ctx database.TenantContext, period string) ([]ServiceBreakdown, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	now := time.Now().UTC()
	var since time.Time
	switch period {
	case "daily":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case "weekly":
		since = now.AddDate(0, 0, -7)
	default:
		// monthly
		since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}

	rows, err := conn.Query(ctx, `
		SELECT
			COALESCE(service, 'unknown') AS service_name,
			COALESCE(model_used, 'unknown') AS model,
			COALESCE(SUM(tokens_used), 0) AS tokens,
			COUNT(*) AS calls,
			COALESCE(AVG(latency_ms), 0) AS avg_latency_ms
		FROM ai_audit_log
		WHERE tenant_id = $1 AND created_at >= $2
		GROUP BY service, model_used
		ORDER BY tokens DESC
	`, ctx.TenantID, since)
	if err != nil {
		return nil, fmt.Errorf("querying usage breakdown: %w", err)
	}
	defer rows.Close()

	var totalTokens int64
	var breakdowns []ServiceBreakdown

	for rows.Next() {
		var b ServiceBreakdown
		if err := rows.Scan(&b.ServiceName, &b.Model, &b.Tokens, &b.Calls, &b.AvgLatencyMs); err != nil {
			return nil, fmt.Errorf("scanning usage breakdown row: %w", err)
		}
		totalTokens += b.Tokens
		breakdowns = append(breakdowns, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating usage breakdown rows: %w", err)
	}

	// Calculate usage percentages.
	if totalTokens > 0 {
		for i := range breakdowns {
			breakdowns[i].UsagePercent = float64(breakdowns[i].Tokens) / float64(totalTokens) * 100
		}
	}

	return breakdowns, nil
}
