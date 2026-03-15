package behavior

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// ProductivityMetrics holds computed productivity indicators.
type ProductivityMetrics struct {
	PeakDay             string   `json:"peak_day"`
	AvgDecisionTimeMins *float64 `json:"avg_decision_time_minutes"`
	DelegationRatePct   float64  `json:"delegation_rate_pct"`
	InboxZeroDays       int      `json:"inbox_zero_days"`
	HourlyDistribution  [24]int  `json:"hourly_distribution"`
}

// ComputeProductivityMetrics computes all productivity metrics for a user.
func (s *BehaviorService) ComputeProductivityMetrics(ctx database.TenantContext, userID uuid.UUID, date time.Time) (*ProductivityMetrics, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	metrics := &ProductivityMetrics{}

	// Peak day: day of week with highest sent email count over 30 days
	var peakDow *int
	err = conn.QueryRow(ctx,
		`SELECT EXTRACT(DOW FROM created_at)::int as dow
		 FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND created_at >= $2::date - interval '30 days'
		 GROUP BY dow
		 ORDER BY COUNT(*) DESC
		 LIMIT 1`,
		userID, date.Format("2006-01-02"),
	).Scan(&peakDow)
	if err == nil && peakDow != nil {
		days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
		if *peakDow >= 0 && *peakDow < 7 {
			metrics.PeakDay = days[*peakDow]
		}
	}

	// Decision time: avg response time for emails where action was required
	var avgDecision *float64
	_ = conn.QueryRow(ctx,
		`SELECT AVG(e.response_time_minutes)
		 FROM emails e
		 JOIN email_summaries es ON es.email_id = e.id
		 WHERE e.user_id = $1
		   AND e.response_time_minutes IS NOT NULL
		   AND es.action_required = true
		   AND e.created_at >= $2::date - interval '30 days'`,
		userID, date.Format("2006-01-02"),
	).Scan(&avgDecision)
	metrics.AvgDecisionTimeMins = avgDecision

	// Delegation rate: % of emails forwarded vs replied directly
	var totalOutbound, forwarded int
	_ = conn.QueryRow(ctx,
		`SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE subject ILIKE 'Fwd:%' OR subject ILIKE 'Fw:%') as forwarded
		 FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND created_at >= $2::date - interval '30 days'`,
		userID, date.Format("2006-01-02"),
	).Scan(&totalOutbound, &forwarded)
	if totalOutbound > 0 {
		metrics.DelegationRatePct = float64(forwarded) / float64(totalOutbound) * 100
	}

	// Inbox zero days: days in last 30 where no emails remained in needs-reply state
	var inboxZeroDays int
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(DISTINCT d.date)
		 FROM generate_series(
			$2::date - interval '30 days',
			$2::date - interval '1 day',
			interval '1 day'
		 ) d(date)
		 WHERE NOT EXISTS (
			SELECT 1 FROM emails e
			JOIN email_summaries es ON es.email_id = e.id
			WHERE e.user_id = $1
			  AND e.direction = 'inbound'
			  AND es.action_required = true
			  AND e.created_at::date = d.date
			  AND NOT EXISTS (
				SELECT 1 FROM emails r
				WHERE r.in_reply_to = e.message_id
				  AND r.user_id = $1
				  AND r.direction = 'outbound'
			  )
		 )`,
		userID, date.Format("2006-01-02"),
	).Scan(&inboxZeroDays)
	metrics.InboxZeroDays = inboxZeroDays

	// Hourly distribution: reuse ComputePeakHours over 30 days
	timezone := s.getUserTimezone(ctx, userID)
	peakHours, _ := s.ComputePeakHours(ctx, userID, date.AddDate(0, 0, -30), date.AddDate(0, 0, 1), timezone)
	metrics.HourlyDistribution = peakHours

	return metrics, nil
}
