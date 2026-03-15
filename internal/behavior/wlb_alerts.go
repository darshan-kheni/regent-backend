package behavior

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/briefings"
	"github.com/darshan-kheni/regent/internal/database"
)

// CheckWLBAlerts checks if WLB alert conditions are met and generates alerts.
// Trigger 1: Score dropped > 10 points in 7 days
// Trigger 2: Score < 50 for 3+ consecutive days
// Rate limit: max 1 alert per 7 days
func (s *BehaviorService) CheckWLBAlerts(ctx database.TenantContext, userID uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Check rate limit: skip if last alert was less than 7 days ago
	var lastAlert *time.Time
	err = conn.QueryRow(ctx,
		`SELECT last_wlb_alert FROM behavior_profiles WHERE user_id = $1`,
		userID,
	).Scan(&lastAlert)
	if err == nil && lastAlert != nil && time.Since(*lastAlert) < 7*24*time.Hour {
		return nil // Rate limited
	}

	// Get last 7 days of WLB snapshots
	rows, err := conn.Query(ctx,
		`SELECT date, score FROM wlb_snapshots
		 WHERE user_id = $1 AND date >= now()::date - interval '7 days'
		 ORDER BY date DESC`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("querying WLB snapshots: %w", err)
	}
	defer rows.Close()

	type snapshot struct {
		date  time.Time
		score int
	}
	var snapshots []snapshot
	for rows.Next() {
		var snap snapshot
		if err := rows.Scan(&snap.date, &snap.score); err != nil {
			continue
		}
		snapshots = append(snapshots, snap)
	}

	if len(snapshots) < 2 {
		return nil // Not enough data
	}

	triggered := false
	var reason string

	// Trigger 1: Score dropped > 10 points in 7 days
	newest := snapshots[0].score
	oldest := snapshots[len(snapshots)-1].score
	if oldest-newest > 10 {
		triggered = true
		reason = fmt.Sprintf("WLB score dropped %d points in 7 days (from %d to %d)", oldest-newest, oldest, newest)
	}

	// Trigger 2: Score < 50 for 3+ consecutive days
	if !triggered {
		consecutiveLow := 0
		for _, snap := range snapshots {
			if snap.score < 50 {
				consecutiveLow++
			} else {
				break
			}
		}
		if consecutiveLow >= 3 {
			triggered = true
			reason = fmt.Sprintf("WLB score below 50 for %d consecutive days (current: %d)", consecutiveLow, snapshots[0].score)
		}
	}

	if !triggered {
		return nil
	}

	// Get current metrics for the alert prompt
	var afterHoursPct float64
	var weekendEmails int
	_ = conn.QueryRow(ctx,
		`SELECT COALESCE(after_hours_pct, 0), COALESCE(weekend_emails, 0)
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'daily'
		 ORDER BY period_start DESC LIMIT 1`,
		userID,
	).Scan(&afterHoursPct, &weekendEmails)

	// Generate alert via ministral-3:8b
	if s.ai != nil {
		prompt := fmt.Sprintf(
			`Given this WLB data: after_hours=%.0f%%, weekend_emails=%d, trigger=%s. Generate a 50-word wellness alert for a high-net-worth professional. Cite specific factors. Suggest one concrete action.`,
			afterHoursPct, weekendEmails, reason,
		)

		resp, aiErr := s.ai.Complete(ctx, ai.CompletionRequest{
			ModelID:   "ministral-3:8b",
			Messages:  []ai.Message{{Role: "user", Content: prompt}},
			MaxTokens: 100,
			Format:    "text",
		})

		alertText := reason // fallback if AI fails
		if aiErr == nil && resp != nil && resp.Content != "" {
			alertText = resp.Content
		} else if aiErr != nil {
			slog.Warn("WLB alert AI call failed, using reason text", "error", aiErr)
		}

		// Deliver via Private Briefings
		if s.rdb != nil {
			pubErr := briefings.PublishNotificationEvent(ctx, s.rdb, briefings.NotificationEvent{
				UserID:   userID.String(),
				TenantID: ctx.TenantID.String(),
				Priority: 70,
				Category: "wellness",
				Subject:  "Work-Life Balance Alert",
				Summary:  alertText,
				Channels: []string{"email", "push"},
			})
			if pubErr != nil {
				slog.Warn("failed to publish WLB alert", "error", pubErr)
			}
		}
	}

	// Update last_wlb_alert timestamp
	_, err = conn.Exec(ctx,
		`UPDATE behavior_profiles SET last_wlb_alert = now() WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		slog.Warn("failed to update last_wlb_alert", "error", err)
	}

	return nil
}
