package behavior

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// computeResponseTimeTrend compares this week's avg response time to 4-week rolling avg.
// ok < 10%, warn 10-25%, critical > 25%
func (s *BehaviorService) computeResponseTimeTrend(ctx database.TenantContext, userID uuid.UUID, date time.Time) StressIndicator {
	ind := StressIndicator{Metric: "response_time_trend", Status: "ok"}

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

	// This week's avg response time
	weekStart := date.AddDate(0, 0, -int(date.Weekday()))
	var currentAvg *float64
	conn.QueryRow(ctx,
		`SELECT AVG(avg_response_time_minutes)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'daily'
		   AND period_start >= $2 AND period_start <= $3`,
		userID, weekStart.Format("2006-01-02"), date.Format("2006-01-02"),
	).Scan(&currentAvg)

	// 4-week rolling avg (excluding this week)
	fourWeeksAgo := weekStart.AddDate(0, 0, -28)
	var rollingAvg *float64
	conn.QueryRow(ctx,
		`SELECT AVG(avg_response_time_minutes)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'daily'
		   AND period_start >= $2 AND period_start < $3`,
		userID, fourWeeksAgo.Format("2006-01-02"), weekStart.Format("2006-01-02"),
	).Scan(&rollingAvg)

	if currentAvg == nil || rollingAvg == nil || *rollingAvg == 0 {
		ind.Value = "N/A"
		ind.Detail = "Insufficient data for response time trend"
		return ind
	}

	delta := (*currentAvg - *rollingAvg) / *rollingAvg * 100
	ind.Value = fmt.Sprintf("%.1f min", *currentAvg)
	if delta >= 0 {
		ind.Delta = fmt.Sprintf("+%.0f%%", delta)
	} else {
		ind.Delta = fmt.Sprintf("%.0f%%", delta)
	}

	absDelta := math.Abs(delta)
	switch {
	case delta > 0 && absDelta > 25:
		ind.Status = "critical"
		ind.Detail = fmt.Sprintf("Response time increased %.0f%% vs 4-week average", absDelta)
	case delta > 0 && absDelta > 10:
		ind.Status = "warn"
		ind.Detail = fmt.Sprintf("Response time slightly elevated (+%.0f%%)", absDelta)
	default:
		ind.Status = "ok"
		ind.Detail = "Response time within normal range"
	}

	return ind
}
