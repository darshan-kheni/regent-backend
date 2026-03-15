package behavior

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/briefings"
	"github.com/darshan-kheni/regent/internal/database"
)

// GenerateWeeklyReport creates a wellness report for Privy Council + Estate users.
// Uses gpt-oss:120b (~800 tokens) to generate a 200-word executive wellness brief.
func (s *BehaviorService) GenerateWeeklyReport(ctx database.TenantContext, userID uuid.UUID) error {
	if s.ai == nil {
		return fmt.Errorf("AI provider not configured")
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Gather last 7 days of data
	now := time.Now()
	weekAgo := now.AddDate(0, 0, -7)
	weekStart := time.Date(weekAgo.Year(), weekAgo.Month(), weekAgo.Day(), 0, 0, 0, 0, weekAgo.Location())

	dataStr := s.formatReportData(ctx, conn, userID, weekStart, now)

	// Call gpt-oss:120b
	prompt := fmt.Sprintf(WellnessReportPrompt, dataStr)

	resp, err := s.ai.Complete(ctx, ai.CompletionRequest{
		ModelID:   "gpt-oss:120b",
		Messages:  []ai.Message{{Role: "user", Content: prompt}},
		MaxTokens: 400,
		Format:    "text",
	})
	if err != nil {
		return fmt.Errorf("generating wellness report: %w", err)
	}

	reportText := resp.Content
	tokensUsed := resp.TokensIn + resp.TokensOut

	// Parse insights from the report
	insights := map[string]string{
		"report_length": fmt.Sprintf("%d words", len(strings.Fields(reportText))),
		"model":         resp.Model,
	}
	insightsJSON, _ := json.Marshal(insights)

	// UPSERT into wellness_reports
	mondayDate := weekStart.Format("2006-01-02")
	_, err = conn.Exec(ctx,
		`INSERT INTO wellness_reports (tenant_id, user_id, week_start, report_text, model_used, tokens_used, insights)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (user_id, week_start) DO UPDATE SET
			report_text = EXCLUDED.report_text,
			model_used = EXCLUDED.model_used,
			tokens_used = EXCLUDED.tokens_used,
			insights = EXCLUDED.insights`,
		ctx.TenantID, userID, mondayDate,
		reportText, resp.Model, tokensUsed, insightsJSON,
	)
	if err != nil {
		return fmt.Errorf("storing wellness report: %w", err)
	}

	// Deliver via Private Briefings
	if s.rdb != nil {
		pubErr := briefings.PublishNotificationEvent(ctx, s.rdb, briefings.NotificationEvent{
			UserID:   userID.String(),
			TenantID: ctx.TenantID.String(),
			Priority: 40,
			Category: "wellness",
			Subject:  "Weekly Wellness Report",
			Summary:  reportText,
			Channels: []string{"email"},
		})
		if pubErr != nil {
			slog.Warn("failed to publish wellness report notification", "error", pubErr)
		}
	}

	return nil
}

// formatReportData gathers and formats behavior data for the wellness report prompt.
func (s *BehaviorService) formatReportData(ctx database.TenantContext, conn *pgxpool.Conn, userID uuid.UUID, weekStart, weekEnd time.Time) string {
	_ = weekEnd // used for context, queries use latest records

	var parts []string

	// WLB score
	var wlbScore *int
	_ = conn.QueryRow(ctx,
		`SELECT wlb_score FROM behavior_profiles WHERE user_id = $1`,
		userID,
	).Scan(&wlbScore)
	if wlbScore != nil {
		parts = append(parts, fmt.Sprintf("WLB Score: %d/100", *wlbScore))
	}

	// Communication metrics (weekly)
	var avgRT *float64
	var sent, received, weekendEmails int
	var afterHoursPct *float64
	_ = conn.QueryRow(ctx,
		`SELECT avg_response_time_minutes, emails_sent, emails_received, after_hours_pct, weekend_emails
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'weekly'
		 ORDER BY period_start DESC LIMIT 1`,
		userID,
	).Scan(&avgRT, &sent, &received, &afterHoursPct, &weekendEmails)

	if avgRT != nil {
		parts = append(parts, fmt.Sprintf("Avg response time: %.1f min", *avgRT))
	}
	parts = append(parts, fmt.Sprintf("Emails sent: %d, received: %d", sent, received))
	if afterHoursPct != nil {
		parts = append(parts, fmt.Sprintf("After-hours emails: %.0f%%", *afterHoursPct))
	}
	parts = append(parts, fmt.Sprintf("Weekend emails: %d", weekendEmails))

	// Stress indicators
	stressRows, err := conn.Query(ctx,
		`SELECT metric, status, value FROM stress_indicators
		 WHERE user_id = $1 ORDER BY date DESC LIMIT 5`,
		userID,
	)
	if err == nil {
		defer stressRows.Close()
		for stressRows.Next() {
			var metric, status, value string
			if stressRows.Scan(&metric, &status, &value) == nil {
				parts = append(parts, fmt.Sprintf("Stress - %s: %s (%s)", metric, value, status))
			}
		}
	}

	// Top declining contact
	var contactName *string
	var contactEmail string
	var freq string
	err = conn.QueryRow(ctx,
		`SELECT contact_email, contact_name, interaction_frequency
		 FROM contact_relationships
		 WHERE user_id = $1 AND is_declining = true
		 ORDER BY interaction_count DESC LIMIT 1`,
		userID,
	).Scan(&contactEmail, &contactName, &freq)
	if err == nil {
		name := contactEmail
		if contactName != nil && *contactName != "" {
			name = *contactName
		}
		parts = append(parts, fmt.Sprintf("Declining relationship: %s (now %s)", name, freq))
	}

	if len(parts) == 0 {
		return "No behavioral data available for this week."
	}
	return strings.Join(parts, "\n")
}
