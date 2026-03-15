package behavior

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// StressIndicator represents a single stress metric with its computed status.
type StressIndicator struct {
	Metric string `json:"metric"`
	Value  string `json:"value"`
	Delta  string `json:"delta"`
	Status string `json:"status"` // ok, warn, critical
	Detail string `json:"detail"`
}

// ComputeStressIndicators computes all 5 stress indicators for a user.
func (s *BehaviorService) ComputeStressIndicators(ctx database.TenantContext, userID uuid.UUID, date time.Time) ([]StressIndicator, error) {
	indicators := make([]StressIndicator, 0, 5)

	rt := s.computeResponseTimeTrend(ctx, userID, date)
	indicators = append(indicators, rt)

	night := s.computeLateNightActivity(ctx, userID, date)
	indicators = append(indicators, night)

	vol := s.computeEmailVolume(ctx, userID, date)
	indicators = append(indicators, vol)

	tone := s.computeToneConsistency(ctx, userID, date)
	indicators = append(indicators, tone)

	weekend := s.computeWeekendBoundary(ctx, userID, date)
	indicators = append(indicators, weekend)

	// UPSERT all indicators
	if err := s.storeStressIndicators(ctx, userID, date, indicators); err != nil {
		slog.Warn("failed to store stress indicators", "user_id", userID, "error", err)
	}

	return indicators, nil
}

// storeStressIndicators UPSERTs indicators into the stress_indicators table.
func (s *BehaviorService) storeStressIndicators(ctx database.TenantContext, userID uuid.UUID, date time.Time, indicators []StressIndicator) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	for _, ind := range indicators {
		_, err := conn.Exec(ctx,
			`INSERT INTO stress_indicators (tenant_id, user_id, date, metric, value, delta, status, detail)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (user_id, date, metric) DO UPDATE SET
				value = EXCLUDED.value,
				delta = EXCLUDED.delta,
				status = EXCLUDED.status,
				detail = EXCLUDED.detail`,
			ctx.TenantID, userID, date.Format("2006-01-02"),
			ind.Metric, ind.Value, ind.Delta, ind.Status, ind.Detail,
		)
		if err != nil {
			slog.Warn("failed to upsert stress indicator",
				"metric", ind.Metric, "error", err)
		}
	}
	return nil
}
