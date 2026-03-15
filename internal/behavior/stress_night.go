package behavior

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// computeLateNightActivity compares 21:00-06:00 email ratio to 30-day baseline.
// ok < baseline, warn 1.5x baseline, critical > 2x baseline
func (s *BehaviorService) computeLateNightActivity(ctx database.TenantContext, userID uuid.UUID, date time.Time) StressIndicator {
	ind := StressIndicator{Metric: "late_night_activity", Status: "ok"}

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

	timezone := s.getUserTimezone(ctx, userID)

	// Last 7 days: count total and late-night emails
	weekAgo := date.AddDate(0, 0, -7)
	var total, nightCount int
	conn.QueryRow(ctx,
		`SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (
				WHERE EXTRACT(HOUR FROM created_at AT TIME ZONE $4) >= 21
				   OR EXTRACT(HOUR FROM created_at AT TIME ZONE $4) < 6
			) as night_count
		 FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND created_at >= $2 AND created_at < $3`,
		userID, weekAgo.Format("2006-01-02"), date.AddDate(0, 0, 1).Format("2006-01-02"), timezone,
	).Scan(&total, &nightCount)

	if total == 0 {
		ind.Value = "0%"
		ind.Detail = "No outbound emails in the last 7 days"
		return ind
	}

	currentPct := float64(nightCount) / float64(total) * 100

	// 30-day baseline
	thirtyDaysAgo := date.AddDate(0, 0, -30)
	var baseTotal, baseNight int
	conn.QueryRow(ctx,
		`SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (
				WHERE EXTRACT(HOUR FROM created_at AT TIME ZONE $4) >= 21
				   OR EXTRACT(HOUR FROM created_at AT TIME ZONE $4) < 6
			) as night_count
		 FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND created_at >= $2 AND created_at < $3`,
		userID, thirtyDaysAgo.Format("2006-01-02"), weekAgo.Format("2006-01-02"), timezone,
	).Scan(&baseTotal, &baseNight)

	baselinePct := 0.01 // avoid division by zero
	if baseTotal > 0 {
		baselinePct = float64(baseNight) / float64(baseTotal) * 100
		if baselinePct < 0.01 {
			baselinePct = 0.01
		}
	}

	ratio := currentPct / baselinePct
	ind.Value = fmt.Sprintf("%.0f%%", currentPct)

	switch {
	case ratio > 2.0:
		ind.Status = "critical"
		ind.Delta = fmt.Sprintf("%.1fx baseline", ratio)
		ind.Detail = fmt.Sprintf("Late-night activity at %.1fx the 30-day baseline", ratio)
	case ratio > 1.5:
		ind.Status = "warn"
		ind.Delta = fmt.Sprintf("%.1fx baseline", ratio)
		ind.Detail = fmt.Sprintf("Late-night activity elevated (%.1fx baseline)", ratio)
	default:
		ind.Status = "ok"
		ind.Delta = fmt.Sprintf("%.1fx baseline", ratio)
		ind.Detail = "Late-night activity within normal range"
	}

	return ind
}
