package behavior

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// OverviewResponse contains the data for the intelligence overview tab.
type OverviewResponse struct {
	AIUnderstandingScore int               `json:"ai_understanding_score"`
	WLBScore             int               `json:"wlb_score"`
	LastComputed         *time.Time        `json:"last_computed"`
	Calibration          *UserCalibration  `json:"calibration"`
	QuickStats           QuickStats        `json:"quick_stats"`
	StressIndicators     []StressIndicator `json:"stress_indicators"`
	LatestWellnessReport *string           `json:"latest_wellness_report"`
}

// QuickStats contains summary metrics for the overview.
type QuickStats struct {
	EmailsThisWeek      int     `json:"emails_this_week"`
	AvgResponseTimeMins float64 `json:"avg_response_time_minutes"`
	TopContact          string  `json:"top_contact"`
	StreakDays           int     `json:"streak_days"`
}

// WLBResponse contains the data for the WLB tab.
type WLBResponse struct {
	Score                int              `json:"score"`
	Penalties            WLBPenalties     `json:"penalties"`
	Trend7d              []WLBSnapshot    `json:"trend_7d"`
	Trend30d             []WLBSnapshot    `json:"trend_30d"`
	LatestRecommendation *string          `json:"latest_recommendation"`
	Calibration          *UserCalibration `json:"calibration"`
}

// WLBSnapshot is a daily WLB score point for trends.
type WLBSnapshot struct {
	Date  string `json:"date"`
	Score int    `json:"score"`
}

// CommunicationResponse contains communication metrics for a period.
type CommunicationResponse struct {
	PeriodStart           string             `json:"period_start"`
	PeriodType            string             `json:"period_type"`
	AvgResponseTimeMins   float64            `json:"avg_response_time_minutes"`
	AvgEmailLengthWords   int                `json:"avg_email_length_words"`
	EmailsSent            int                `json:"emails_sent"`
	EmailsReceived        int                `json:"emails_received"`
	ToneDistribution      map[string]float64 `json:"tone_distribution"`
	FormalityDistribution map[string]float64 `json:"formality_distribution"`
	PeakHours             []int              `json:"peak_hours"`
	AfterHoursPct         float64            `json:"after_hours_pct"`
	WeekendEmails         int                `json:"weekend_emails"`
}

// WellnessReportResponse is a single wellness report for API responses.
type WellnessReportResponse struct {
	WeekStart  string `json:"week_start"`
	ReportText string `json:"report_text"`
	CreatedAt  string `json:"created_at"`
}

// GetOverview returns behavior profile, stress indicators, and quick stats.
func (s *BehaviorService) GetOverview(ctx database.TenantContext, userID uuid.UUID) (*OverviewResponse, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	resp := &OverviewResponse{}

	// Get behavior profile
	var calJSON []byte
	var lastComputed *time.Time
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(ai_understanding_score, 0), COALESCE(wlb_score, 0), last_computed, calibration
		 FROM behavior_profiles WHERE user_id = $1`, userID,
	).Scan(&resp.AIUnderstandingScore, &resp.WLBScore, &lastComputed, &calJSON)
	if err != nil {
		// No profile yet — return defaults
		resp.AIUnderstandingScore = 0
		resp.WLBScore = 0
	}
	resp.LastComputed = lastComputed
	if len(calJSON) > 0 {
		var cal UserCalibration
		if json.Unmarshal(calJSON, &cal) == nil {
			resp.Calibration = &cal
		}
	}

	// Get latest stress indicators
	rows, err := conn.Query(ctx,
		`SELECT metric, value, delta, status, detail
		 FROM stress_indicators
		 WHERE user_id = $1 AND date = (SELECT MAX(date) FROM stress_indicators WHERE user_id = $1)
		 ORDER BY metric`, userID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var si StressIndicator
			if rows.Scan(&si.Metric, &si.Value, &si.Delta, &si.Status, &si.Detail) == nil {
				resp.StressIndicators = append(resp.StressIndicators, si)
			}
		}
	}
	if resp.StressIndicators == nil {
		resp.StressIndicators = []StressIndicator{}
	}

	// Quick stats: emails this week
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails
		 WHERE user_id = $1 AND created_at >= date_trunc('week', CURRENT_DATE)`, userID,
	).Scan(&resp.QuickStats.EmailsThisWeek)

	// Quick stats: avg response time (last 30 days)
	_ = conn.QueryRow(ctx,
		`SELECT COALESCE(AVG(response_time_minutes), 0)
		 FROM emails WHERE user_id = $1 AND response_time_minutes IS NOT NULL
		 AND created_at >= CURRENT_DATE - interval '30 days'`, userID,
	).Scan(&resp.QuickStats.AvgResponseTimeMins)

	// Quick stats: top contact (most interactions)
	_ = conn.QueryRow(ctx,
		`SELECT COALESCE(contact_email, '') FROM contact_relationships
		 WHERE user_id = $1 ORDER BY interaction_count DESC LIMIT 1`, userID,
	).Scan(&resp.QuickStats.TopContact)

	// Quick stats: streak days (consecutive days with at least 1 email processed)
	_ = conn.QueryRow(ctx,
		`WITH daily AS (
			SELECT DISTINCT created_at::date as d FROM emails WHERE user_id = $1 ORDER BY d DESC
		), numbered AS (
			SELECT d, d - (ROW_NUMBER() OVER (ORDER BY d DESC))::int * interval '1 day' as grp FROM daily
		)
		SELECT COUNT(*) FROM numbered WHERE grp = (SELECT grp FROM numbered LIMIT 1)`, userID,
	).Scan(&resp.QuickStats.StreakDays)

	// Latest wellness report text
	var reportText *string
	_ = conn.QueryRow(ctx,
		`SELECT report_text FROM wellness_reports
		 WHERE user_id = $1 ORDER BY week_start DESC LIMIT 1`, userID,
	).Scan(&reportText)
	resp.LatestWellnessReport = reportText

	return resp, nil
}

// GetCommunicationMetrics returns the latest communication metrics for a period type.
func (s *BehaviorService) GetCommunicationMetrics(ctx database.TenantContext, userID uuid.UUID, periodType string) (*CommunicationResponse, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	resp := &CommunicationResponse{PeriodType: periodType}

	var toneJSON, formalityJSON, peakJSON []byte
	err = conn.QueryRow(ctx,
		`SELECT period_start, COALESCE(avg_response_time_minutes, 0), COALESCE(avg_email_length_words, 0),
		        COALESCE(emails_sent, 0), COALESCE(emails_received, 0),
		        tone_distribution, formality_distribution, peak_hours,
		        COALESCE(after_hours_pct, 0), COALESCE(weekend_emails, 0)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = $2
		 ORDER BY period_start DESC LIMIT 1`, userID, periodType,
	).Scan(&resp.PeriodStart, &resp.AvgResponseTimeMins, &resp.AvgEmailLengthWords,
		&resp.EmailsSent, &resp.EmailsReceived,
		&toneJSON, &formalityJSON, &peakJSON,
		&resp.AfterHoursPct, &resp.WeekendEmails)
	if err != nil {
		// No data yet — return empty
		resp.ToneDistribution = map[string]float64{}
		resp.FormalityDistribution = map[string]float64{}
		resp.PeakHours = make([]int, 24)
		return resp, nil
	}

	resp.ToneDistribution = map[string]float64{}
	if len(toneJSON) > 0 {
		_ = json.Unmarshal(toneJSON, &resp.ToneDistribution)
	}

	resp.FormalityDistribution = map[string]float64{}
	if len(formalityJSON) > 0 {
		_ = json.Unmarshal(formalityJSON, &resp.FormalityDistribution)
	}

	resp.PeakHours = make([]int, 24)
	if len(peakJSON) > 0 {
		_ = json.Unmarshal(peakJSON, &resp.PeakHours)
	}

	return resp, nil
}

// GetWLBData returns WLB score, penalties breakdown, and trend history.
func (s *BehaviorService) GetWLBData(ctx database.TenantContext, userID uuid.UUID) (*WLBResponse, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	resp := &WLBResponse{}

	// Get current score
	_ = conn.QueryRow(ctx,
		`SELECT COALESCE(wlb_score, 0) FROM behavior_profiles WHERE user_id = $1`, userID,
	).Scan(&resp.Score)

	// Get latest snapshot penalties
	_ = conn.QueryRow(ctx,
		`SELECT after_hours_penalty, weekend_penalty, boundary_penalty, volume_penalty
		 FROM wlb_snapshots WHERE user_id = $1 ORDER BY date DESC LIMIT 1`, userID,
	).Scan(&resp.Penalties.AfterHours, &resp.Penalties.Weekend, &resp.Penalties.Boundary, &resp.Penalties.Volume)

	// 7-day trend
	rows7, err := conn.Query(ctx,
		`SELECT date::text, score FROM wlb_snapshots
		 WHERE user_id = $1 AND date >= CURRENT_DATE - interval '7 days'
		 ORDER BY date`, userID,
	)
	if err == nil {
		defer rows7.Close()
		for rows7.Next() {
			var snap WLBSnapshot
			if rows7.Scan(&snap.Date, &snap.Score) == nil {
				resp.Trend7d = append(resp.Trend7d, snap)
			}
		}
	}
	if resp.Trend7d == nil {
		resp.Trend7d = []WLBSnapshot{}
	}

	// 30-day trend
	rows30, err := conn.Query(ctx,
		`SELECT date::text, score FROM wlb_snapshots
		 WHERE user_id = $1 AND date >= CURRENT_DATE - interval '30 days'
		 ORDER BY date`, userID,
	)
	if err == nil {
		defer rows30.Close()
		for rows30.Next() {
			var snap WLBSnapshot
			if rows30.Scan(&snap.Date, &snap.Score) == nil {
				resp.Trend30d = append(resp.Trend30d, snap)
			}
		}
	}
	if resp.Trend30d == nil {
		resp.Trend30d = []WLBSnapshot{}
	}

	// Latest recommendation (from wellness reports)
	var rec *string
	_ = conn.QueryRow(ctx,
		`SELECT report_text FROM wellness_reports
		 WHERE user_id = $1 ORDER BY week_start DESC LIMIT 1`, userID,
	).Scan(&rec)
	resp.LatestRecommendation = rec

	// Calibration
	cal, _ := s.LoadCalibration(ctx, userID)
	resp.Calibration = cal

	return resp, nil
}

// GetStressIndicators returns current stress indicators for a user.
func (s *BehaviorService) GetStressIndicators(ctx database.TenantContext, userID uuid.UUID) ([]StressIndicator, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT metric, value, delta, status, detail
		 FROM stress_indicators
		 WHERE user_id = $1 AND date = (SELECT MAX(date) FROM stress_indicators WHERE user_id = $1)
		 ORDER BY metric`, userID,
	)
	if err != nil {
		return []StressIndicator{}, nil
	}
	defer rows.Close()

	var indicators []StressIndicator
	for rows.Next() {
		var si StressIndicator
		if rows.Scan(&si.Metric, &si.Value, &si.Delta, &si.Status, &si.Detail) == nil {
			indicators = append(indicators, si)
		}
	}
	if indicators == nil {
		indicators = []StressIndicator{}
	}
	return indicators, nil
}

// GetContactRelationships returns paginated, sorted contact relationships.
func (s *BehaviorService) GetContactRelationships(ctx database.TenantContext, userID uuid.UUID, sortBy string, limit, offset int) ([]ContactRelationship, int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, 0, fmt.Errorf("setting RLS context: %w", err)
	}

	// Get total count
	var total int
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM contact_relationships WHERE user_id = $1`, userID,
	).Scan(&total)

	// Map sort_by to SQL column (prevent SQL injection)
	orderCol := "interaction_count DESC"
	switch sortBy {
	case "response_time":
		orderCol = "avg_response_time_minutes ASC NULLS LAST"
	case "last_interaction":
		orderCol = "last_interaction DESC NULLS LAST"
	}

	rows, err := conn.Query(ctx,
		fmt.Sprintf(`SELECT contact_email, contact_name, interaction_count,
		        avg_response_time_minutes, dominant_tone, sentiment_trend,
		        interaction_frequency, last_interaction, first_interaction, is_declining
		 FROM contact_relationships
		 WHERE user_id = $1
		 ORDER BY %s
		 LIMIT $2 OFFSET $3`, orderCol), userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("querying contact relationships: %w", err)
	}
	defer rows.Close()

	var contacts []ContactRelationship
	for rows.Next() {
		var c ContactRelationship
		if err := rows.Scan(&c.ContactEmail, &c.ContactName, &c.InteractionCount,
			&c.AvgResponseTimeMin, &c.DominantTone, &c.SentimentTrend,
			&c.InteractionFrequency, &c.LastInteraction, &c.FirstInteraction, &c.IsDeclining); err != nil {
			continue
		}
		contacts = append(contacts, c)
	}
	if contacts == nil {
		contacts = []ContactRelationship{}
	}

	return contacts, total, nil
}

// GetProductivityMetrics returns computed productivity metrics for the API.
func (s *BehaviorService) GetProductivityMetrics(ctx database.TenantContext, userID uuid.UUID) (*ProductivityMetrics, error) {
	return s.ComputeProductivityMetrics(ctx, userID, time.Now())
}

// GetWellnessReports returns the most recent wellness reports.
func (s *BehaviorService) GetWellnessReports(ctx database.TenantContext, userID uuid.UUID, limit int) ([]WellnessReportResponse, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT week_start::text, report_text, created_at
		 FROM wellness_reports
		 WHERE user_id = $1
		 ORDER BY week_start DESC
		 LIMIT $2`, userID, limit,
	)
	if err != nil {
		return []WellnessReportResponse{}, nil
	}
	defer rows.Close()

	var reports []WellnessReportResponse
	for rows.Next() {
		var r WellnessReportResponse
		var createdAt time.Time
		if rows.Scan(&r.WeekStart, &r.ReportText, &createdAt) == nil {
			r.CreatedAt = createdAt.Format(time.RFC3339)
			reports = append(reports, r)
		}
	}
	if reports == nil {
		reports = []WellnessReportResponse{}
	}
	return reports, nil
}
