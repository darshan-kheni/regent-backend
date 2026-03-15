package behavior

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// ComputeCommunicationMetrics computes and stores all communication metrics for a single day.
func (s *BehaviorService) ComputeCommunicationMetrics(ctx database.TenantContext, userID uuid.UUID, date time.Time) error {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)

	// Get user timezone
	timezone := s.getUserTimezone(ctx, userID)

	// Compute individual metrics
	toneDist, err := s.ComputeToneDistribution(ctx, userID, dayStart, dayEnd)
	if err != nil {
		slog.Warn("failed to compute tone distribution", "user_id", userID, "error", err)
		toneDist = map[string]float64{}
	}

	formalityDist, err := s.ComputeFormality(ctx, userID, dayStart, dayEnd)
	if err != nil {
		slog.Warn("failed to compute formality", "user_id", userID, "error", err)
		formalityDist = map[string]float64{}
	}

	peakHours, err := s.ComputePeakHours(ctx, userID, dayStart, dayEnd, timezone)
	if err != nil {
		slog.Warn("failed to compute peak hours", "user_id", userID, "error", err)
	}

	// Compute response times for the day
	if rtErr := s.ComputeResponseTimes(ctx, userID, dayStart); rtErr != nil {
		slog.Warn("failed to compute response times", "user_id", userID, "error", rtErr)
	}

	// Aggregate email counts and response time
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	var sent, received int
	var avgResponseTime *float64
	var avgLength *int
	err = conn.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN direction = 'outbound' THEN 1 ELSE 0 END), 0) as sent,
			COALESCE(SUM(CASE WHEN direction = 'inbound' THEN 1 ELSE 0 END), 0) as received,
			AVG(response_time_minutes) FILTER (WHERE response_time_minutes IS NOT NULL) as avg_rt,
			AVG(LENGTH(body_text) / 5) FILTER (WHERE direction = 'outbound' AND body_text IS NOT NULL) as avg_len
		 FROM emails
		 WHERE user_id = $1
		   AND created_at >= $2 AND created_at < $3`,
		userID, dayStart, dayEnd,
	).Scan(&sent, &received, &avgResponseTime, &avgLength)
	if err != nil {
		return fmt.Errorf("querying email aggregates: %w", err)
	}

	// Compute after-hours percentage
	afterHoursPct := s.computeAfterHoursPct(ctx, conn, userID, dayStart, dayEnd, timezone)

	// Compute weekend email count
	weekendEmails := s.computeWeekendEmails(ctx, conn, userID, dayStart, dayEnd)

	// Marshal JSONB fields
	toneJSON, _ := json.Marshal(toneDist)
	formalityJSON, _ := json.Marshal(formalityDist)
	peakJSON, _ := json.Marshal(peakHours)

	// UPSERT daily metrics
	_, err = conn.Exec(ctx,
		`INSERT INTO communication_metrics (
			tenant_id, user_id, period_start, period_end, period_type,
			avg_response_time_minutes, avg_email_length_words,
			emails_sent, emails_received,
			tone_distribution, formality_distribution, peak_hours,
			after_hours_pct, weekend_emails, updated_at
		) VALUES ($1, $2, $3, $4, 'daily', $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
		ON CONFLICT (user_id, period_start, period_type) DO UPDATE SET
			avg_response_time_minutes = EXCLUDED.avg_response_time_minutes,
			avg_email_length_words = EXCLUDED.avg_email_length_words,
			emails_sent = EXCLUDED.emails_sent,
			emails_received = EXCLUDED.emails_received,
			tone_distribution = EXCLUDED.tone_distribution,
			formality_distribution = EXCLUDED.formality_distribution,
			peak_hours = EXCLUDED.peak_hours,
			after_hours_pct = EXCLUDED.after_hours_pct,
			weekend_emails = EXCLUDED.weekend_emails,
			updated_at = now()`,
		ctx.TenantID, userID,
		dayStart.Format("2006-01-02"),
		dayEnd.Format("2006-01-02"),
		avgResponseTime, avgLength,
		sent, received,
		toneJSON, formalityJSON, peakJSON,
		afterHoursPct, weekendEmails,
	)
	if err != nil {
		return fmt.Errorf("upserting communication metrics: %w", err)
	}

	return nil
}

// AggregateMetrics rolls daily metrics into weekly and monthly periods.
func (s *BehaviorService) AggregateMetrics(ctx database.TenantContext, userID uuid.UUID, date time.Time) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Weekly aggregation (Monday-Sunday containing this date)
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}
	weekStart := date.AddDate(0, 0, -(weekday - 1))
	weekEnd := weekStart.AddDate(0, 0, 7)

	if err := s.aggregatePeriod(ctx, conn, userID, weekStart, weekEnd, "weekly"); err != nil {
		slog.Warn("failed to aggregate weekly metrics", "user_id", userID, "error", err)
	}

	// Monthly aggregation
	monthStart := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)

	if err := s.aggregatePeriod(ctx, conn, userID, monthStart, monthEnd, "monthly"); err != nil {
		slog.Warn("failed to aggregate monthly metrics", "user_id", userID, "error", err)
	}

	return nil
}

// aggregatePeriod computes aggregate metrics from daily records for a given period.
func (s *BehaviorService) aggregatePeriod(ctx database.TenantContext, conn *pgxpool.Conn, userID uuid.UUID, periodStart, periodEnd time.Time, periodType string) error {
	var sent, received, weekendEmails int
	var avgResponseTime, afterHoursPct *float64
	var avgLength *int

	err := conn.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(emails_sent), 0),
			COALESCE(SUM(emails_received), 0),
			AVG(avg_response_time_minutes) FILTER (WHERE avg_response_time_minutes IS NOT NULL),
			AVG(avg_email_length_words) FILTER (WHERE avg_email_length_words IS NOT NULL),
			AVG(after_hours_pct) FILTER (WHERE after_hours_pct IS NOT NULL),
			COALESCE(SUM(weekend_emails), 0)
		 FROM communication_metrics
		 WHERE user_id = $1
		   AND period_type = 'daily'
		   AND period_start >= $2 AND period_start < $3`,
		userID,
		periodStart.Format("2006-01-02"),
		periodEnd.Format("2006-01-02"),
	).Scan(&sent, &received, &avgResponseTime, &avgLength, &afterHoursPct, &weekendEmails)
	if err != nil {
		return fmt.Errorf("querying daily aggregates for %s: %w", periodType, err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO communication_metrics (
			tenant_id, user_id, period_start, period_end, period_type,
			avg_response_time_minutes, avg_email_length_words,
			emails_sent, emails_received,
			after_hours_pct, weekend_emails, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		ON CONFLICT (user_id, period_start, period_type) DO UPDATE SET
			avg_response_time_minutes = EXCLUDED.avg_response_time_minutes,
			avg_email_length_words = EXCLUDED.avg_email_length_words,
			emails_sent = EXCLUDED.emails_sent,
			emails_received = EXCLUDED.emails_received,
			after_hours_pct = EXCLUDED.after_hours_pct,
			weekend_emails = EXCLUDED.weekend_emails,
			updated_at = now()`,
		ctx.TenantID, userID,
		periodStart.Format("2006-01-02"),
		periodEnd.Format("2006-01-02"),
		periodType,
		avgResponseTime, avgLength,
		sent, received,
		afterHoursPct, weekendEmails,
	)
	if err != nil {
		return fmt.Errorf("upserting %s metrics: %w", periodType, err)
	}

	return nil
}

// computeAfterHoursPct calculates the percentage of emails sent outside business hours.
func (s *BehaviorService) computeAfterHoursPct(ctx database.TenantContext, conn *pgxpool.Conn, userID uuid.UUID, dayStart, dayEnd time.Time, timezone string) float64 {
	if timezone == "" {
		timezone = "UTC"
	}

	var total, afterHours int
	err := conn.QueryRow(ctx,
		`SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (
				WHERE EXTRACT(HOUR FROM created_at AT TIME ZONE $4) < 9
				   OR EXTRACT(HOUR FROM created_at AT TIME ZONE $4) >= 18
			) as after_hours
		 FROM emails
		 WHERE user_id = $1
		   AND direction = 'outbound'
		   AND created_at >= $2 AND created_at < $3`,
		userID, dayStart, dayEnd, timezone,
	).Scan(&total, &afterHours)
	if err != nil || total == 0 {
		return 0
	}
	return float64(afterHours) / float64(total) * 100.0
}

// computeWeekendEmails counts emails sent on Saturday/Sunday.
func (s *BehaviorService) computeWeekendEmails(ctx database.TenantContext, conn *pgxpool.Conn, userID uuid.UUID, dayStart, dayEnd time.Time) int {
	var count int
	err := conn.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM emails
		 WHERE user_id = $1
		   AND direction = 'outbound'
		   AND EXTRACT(DOW FROM created_at) IN (0, 6)
		   AND created_at >= $2 AND created_at < $3`,
		userID, dayStart, dayEnd,
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// getUserTimezone returns the user's configured timezone or "UTC" as fallback.
func (s *BehaviorService) getUserTimezone(ctx database.TenantContext, userID uuid.UUID) string {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return "UTC"
	}
	defer conn.Release()

	var tz *string
	err = conn.QueryRow(ctx,
		`SELECT timezone FROM users WHERE id = $1`,
		userID,
	).Scan(&tz)
	if err != nil || tz == nil || *tz == "" {
		return "UTC"
	}
	return *tz
}
