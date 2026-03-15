package metering

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// DailyUsage represents one day's token usage.
type DailyUsage struct {
	Date         time.Time `json:"date"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCalls   int       `json:"total_calls"`
	CacheHits    int       `json:"cache_hits"`
	PremiumCalls int       `json:"premium_calls"`
}

// ModelBreakdown shows usage per model.
type ModelBreakdown struct {
	Model       string `json:"model"`
	TotalTokens int64  `json:"total_tokens"`
	TotalCalls  int    `json:"total_calls"`
	AvgLatency  int64  `json:"avg_latency_ms"`
}

// Analytics provides token usage analytics queries.
type Analytics struct {
	pool *pgxpool.Pool
}

// NewAnalytics creates a new Analytics instance backed by the given connection pool.
func NewAnalytics(pool *pgxpool.Pool) *Analytics {
	return &Analytics{pool: pool}
}

// GetDailyUsage returns daily token usage for a user in a date range.
func (a *Analytics) GetDailyUsage(ctx database.TenantContext, userID uuid.UUID, from, to time.Time) ([]DailyUsage, error) {
	conn, err := a.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT date, SUM(total_tokens), SUM(total_calls), SUM(cache_hits), SUM(premium_calls)
		 FROM token_usage_daily
		 WHERE user_id = $1 AND date BETWEEN $2 AND $3
		 GROUP BY date ORDER BY date`,
		userID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("querying daily usage: %w", err)
	}
	defer rows.Close()

	var results []DailyUsage
	for rows.Next() {
		var d DailyUsage
		if err := rows.Scan(&d.Date, &d.TotalTokens, &d.TotalCalls, &d.CacheHits, &d.PremiumCalls); err != nil {
			return nil, fmt.Errorf("scanning daily usage row: %w", err)
		}
		results = append(results, d)
	}
	return results, rows.Err()
}

// GetModelBreakdown returns token usage broken down by model.
func (a *Analytics) GetModelBreakdown(ctx database.TenantContext, userID uuid.UUID, from, to time.Time) ([]ModelBreakdown, error) {
	conn, err := a.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT model_used, SUM(total_tokens), SUM(total_calls),
		   SUM(total_latency_ms) / NULLIF(SUM(total_calls), 0)
		 FROM token_usage_daily
		 WHERE user_id = $1 AND date BETWEEN $2 AND $3
		 GROUP BY model_used ORDER BY SUM(total_tokens) DESC`,
		userID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("querying model breakdown: %w", err)
	}
	defer rows.Close()

	var results []ModelBreakdown
	for rows.Next() {
		var m ModelBreakdown
		if err := rows.Scan(&m.Model, &m.TotalTokens, &m.TotalCalls, &m.AvgLatency); err != nil {
			return nil, fmt.Errorf("scanning model breakdown row: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}
