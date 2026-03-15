package behavior

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// computeEmailVolume compares today's email count to 30-day rolling average.
// ok < 1.2x, warn 1.2-2x, critical > 2x
func (s *BehaviorService) computeEmailVolume(ctx database.TenantContext, userID uuid.UUID, date time.Time) StressIndicator {
	ind := StressIndicator{Metric: "email_volume", Status: "ok"}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		ind.Detail = "failed to acquire connection"
		return ind
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		ind.Detail = "failed to set RLS context"
		return ind
	}

	// Today's email count
	var todayCount int
	conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails
		 WHERE user_id = $1
		   AND created_at >= $2 AND created_at < $2::date + interval '1 day'`,
		userID, date.Format("2006-01-02"),
	).Scan(&todayCount)

	// 30-day rolling average
	var avgCount float64
	conn.QueryRow(ctx,
		`SELECT COALESCE(AVG(emails_sent + emails_received), 0)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'daily'
		   AND period_start >= $2::date - interval '30 days'
		   AND period_start < $2`,
		userID, date.Format("2006-01-02"),
	).Scan(&avgCount)

	ind.Value = fmt.Sprintf("%d emails", todayCount)

	if avgCount < 1 {
		ind.Detail = "Insufficient baseline data"
		return ind
	}

	ratio := float64(todayCount) / avgCount

	switch {
	case ratio > 2.0:
		ind.Status = "critical"
		ind.Delta = fmt.Sprintf("%.1fx average", ratio)
		ind.Detail = fmt.Sprintf("Email volume at %.1fx the 30-day average", ratio)
	case ratio > 1.2:
		ind.Status = "warn"
		ind.Delta = fmt.Sprintf("%.1fx average", ratio)
		ind.Detail = fmt.Sprintf("Email volume elevated (%.1fx average)", ratio)
	default:
		ind.Status = "ok"
		ind.Delta = fmt.Sprintf("%.1fx average", ratio)
		ind.Detail = "Email volume within normal range"
	}

	return ind
}
