package behavior

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// RunNightly orchestrates all behavior computations for a user.
// Per-step error isolation: each step logs errors and continues.
// Steps 2-7 are independent; only step 2 (WLB) depends on step 1 (communication metrics).
func (s *BehaviorService) RunNightly(ctx database.TenantContext, userID uuid.UUID, yesterday time.Time) error {
	var errs []error

	slog.Info("behavior: starting nightly computation",
		"user_id", userID,
		"date", yesterday.Format("2006-01-02"),
	)

	// 1. Communication metrics (MUST succeed for WLB to be accurate)
	if err := s.ComputeCommunicationMetrics(ctx, userID, yesterday); err != nil {
		errs = append(errs, fmt.Errorf("communication metrics: %w", err))
	}
	if err := s.AggregateMetrics(ctx, userID, yesterday); err != nil {
		errs = append(errs, fmt.Errorf("aggregate metrics: %w", err))
	}

	// 2. WLB score + snapshot (depends on communication metrics)
	if err := s.ComputeAndStoreWLB(ctx, userID, yesterday); err != nil {
		errs = append(errs, fmt.Errorf("wlb: %w", err))
	}

	// 3. WLB alerts (ministral-3:8b if triggered)
	if err := s.CheckWLBAlerts(ctx, userID); err != nil {
		errs = append(errs, fmt.Errorf("wlb alerts: %w", err))
	}

	// 4. Stress indicators (independent)
	if _, err := s.ComputeStressIndicators(ctx, userID, yesterday); err != nil {
		errs = append(errs, fmt.Errorf("stress: %w", err))
	}

	// 5. Contact relationships (independent)
	if err := s.ComputeContactRelationships(ctx, userID, yesterday); err != nil {
		errs = append(errs, fmt.Errorf("relationships: %w", err))
	}
	if err := s.CheckRelationshipAlerts(ctx, userID); err != nil {
		errs = append(errs, fmt.Errorf("relationship alerts: %w", err))
	}

	// 6. Productivity (independent)
	if _, err := s.ComputeProductivityMetrics(ctx, userID, yesterday); err != nil {
		errs = append(errs, fmt.Errorf("productivity: %w", err))
	}

	// 7. AI Understanding Score (independent)
	if err := s.UpdateAIUnderstandingScore(ctx, userID); err != nil {
		errs = append(errs, fmt.Errorf("ai understanding: %w", err))
	}

	// 8. Cleanup old snapshots (independent, >90 days)
	if err := s.CleanupWLBSnapshots(ctx, userID); err != nil {
		errs = append(errs, fmt.Errorf("cleanup: %w", err))
	}

	if len(errs) > 0 {
		slog.Warn("behavior: nightly computation completed with errors",
			"user_id", userID,
			"error_count", len(errs),
		)
	} else {
		slog.Info("behavior: nightly computation completed successfully",
			"user_id", userID,
		)
	}

	return errors.Join(errs...)
}
