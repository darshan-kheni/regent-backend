package behavior

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// WLBInput contains the data needed to compute a WLB score.
type WLBInput struct {
	AfterHoursPct  float64          // % emails sent outside active hours
	WeekendEmails  int              // count of weekend emails
	FirstEmailHour int              // hour of earliest email (0-23)
	LastEmailHour  int              // hour of latest email (0-23)
	LunchBreakPct  float64          // % days with lunch break respected
	DailyVolume    int              // today email count
	Rolling30dAvg  float64          // 30-day average daily volume
	Calibration    *UserCalibration // user adjustments (nullable)
}

// WLBPenalties stores the breakdown of WLB penalty components.
type WLBPenalties struct {
	AfterHours float64 `json:"after_hours"`
	Weekend    float64 `json:"weekend"`
	Boundary   float64 `json:"boundary"`
	Volume     float64 `json:"volume"`
}

// ComputeWLBScore calculates the Work-Life Balance score (0-100).
// Formula: score = 100 - (after_hours_penalty + weekend_penalty + boundary_penalty + volume_penalty)
func ComputeWLBScore(input WLBInput) (int, WLBPenalties) {
	afterHoursPenalty := math.Min(input.AfterHoursPct/50.0*30.0, 30.0)
	weekendPenalty := math.Min(float64(input.WeekendEmails)*2.0, 20.0)

	var boundaryPenalty float64
	if input.FirstEmailHour < 7 {
		boundaryPenalty += 10
	}
	if input.LastEmailHour >= 22 {
		boundaryPenalty += 10
	}
	if input.LunchBreakPct < 50.0 {
		boundaryPenalty += 10
	}

	var volumePenalty float64
	if input.Rolling30dAvg > 0 && float64(input.DailyVolume) > input.Rolling30dAvg*1.5 {
		volumePenalty = 10
	}

	// Apply calibration
	if input.Calibration != nil {
		if input.Calibration.IntentionalLateWorker {
			afterHoursPenalty *= 0.5
		}
		if input.Calibration.WeekendAcceptable {
			weekendPenalty *= 0.5
		}
	}

	total := afterHoursPenalty + weekendPenalty + boundaryPenalty + volumePenalty
	score := 100 - int(math.Round(total))
	if score < 0 {
		score = 0
	}

	return score, WLBPenalties{
		AfterHours: math.Round(afterHoursPenalty*100) / 100,
		Weekend:    math.Round(weekendPenalty*100) / 100,
		Boundary:   math.Round(boundaryPenalty*100) / 100,
		Volume:     math.Round(volumePenalty*100) / 100,
	}
}

// ComputeAndStoreWLB queries metrics, computes WLB score, and stores both
// the current score in behavior_profiles and a daily snapshot in wlb_snapshots.
func (s *BehaviorService) ComputeAndStoreWLB(ctx database.TenantContext, userID uuid.UUID, date time.Time) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	timezone := s.getUserTimezone(ctx, userID)

	// Get yesterday's communication metrics
	var afterHoursPct float64
	var weekendEmails, emailsSent int
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(after_hours_pct, 0), COALESCE(weekend_emails, 0), COALESCE(emails_sent, 0)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_start = $2 AND period_type = 'daily'`,
		userID, date.Format("2006-01-02"),
	).Scan(&afterHoursPct, &weekendEmails, &emailsSent)
	if err != nil {
		// No metrics for this day — skip
		return nil
	}

	// Get first and last email hours
	var firstHour, lastHour *int
	err = conn.QueryRow(ctx,
		`SELECT
			MIN(EXTRACT(HOUR FROM created_at AT TIME ZONE $3))::int,
			MAX(EXTRACT(HOUR FROM created_at AT TIME ZONE $3))::int
		 FROM emails
		 WHERE user_id = $1
		   AND direction = 'outbound'
		   AND created_at >= $2::date AND created_at < $2::date + interval '1 day'`,
		userID, date.Format("2006-01-02"), timezone,
	).Scan(&firstHour, &lastHour)
	if err != nil || firstHour == nil {
		firstHour = intPtr(9)
		lastHour = intPtr(17)
	}

	// Get 30-day rolling average volume
	var rolling30dAvg float64
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(AVG(emails_sent + emails_received), 0)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'daily'
		   AND period_start >= $2::date - interval '30 days'
		   AND period_start < $2`,
		userID, date.Format("2006-01-02"),
	).Scan(&rolling30dAvg)
	if err != nil {
		rolling30dAvg = 0
	}

	// Load calibration
	cal, _ := s.LoadCalibration(ctx, userID)

	// Compute score
	input := WLBInput{
		AfterHoursPct:  afterHoursPct,
		WeekendEmails:  weekendEmails,
		FirstEmailHour: *firstHour,
		LastEmailHour:  *lastHour,
		LunchBreakPct:  100, // TODO: compute lunch break detection
		DailyVolume:    emailsSent,
		Rolling30dAvg:  rolling30dAvg,
		Calibration:    cal,
	}
	score, penalties := ComputeWLBScore(input)

	// UPSERT behavior_profiles
	_, err = conn.Exec(ctx,
		`INSERT INTO behavior_profiles (tenant_id, user_id, wlb_score, last_computed, updated_at)
		 VALUES ($1, $2, $3, now(), now())
		 ON CONFLICT (user_id) DO UPDATE SET
			wlb_score = EXCLUDED.wlb_score,
			last_computed = now(),
			updated_at = now()`,
		ctx.TenantID, userID, score,
	)
	if err != nil {
		return fmt.Errorf("upserting behavior profile WLB: %w", err)
	}

	// UPSERT wlb_snapshots
	_, err = conn.Exec(ctx,
		`INSERT INTO wlb_snapshots (tenant_id, user_id, date, score, after_hours_penalty, weekend_penalty, boundary_penalty, volume_penalty)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (user_id, date) DO UPDATE SET
			score = EXCLUDED.score,
			after_hours_penalty = EXCLUDED.after_hours_penalty,
			weekend_penalty = EXCLUDED.weekend_penalty,
			boundary_penalty = EXCLUDED.boundary_penalty,
			volume_penalty = EXCLUDED.volume_penalty`,
		ctx.TenantID, userID, date.Format("2006-01-02"),
		score, penalties.AfterHours, penalties.Weekend, penalties.Boundary, penalties.Volume,
	)
	if err != nil {
		return fmt.Errorf("upserting WLB snapshot: %w", err)
	}

	return nil
}

// CleanupWLBSnapshots removes snapshots older than 90 days.
func (s *BehaviorService) CleanupWLBSnapshots(ctx database.TenantContext, userID uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`DELETE FROM wlb_snapshots WHERE user_id = $1 AND date < now() - interval '90 days'`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("cleaning up WLB snapshots: %w", err)
	}
	return nil
}

func intPtr(v int) *int {
	return &v
}
