package behavior

import (
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/briefings"
	"github.com/darshan-kheni/regent/internal/database"
)

// CheckRelationshipAlerts checks for declining relationships and VIP response time increases.
func (s *BehaviorService) CheckRelationshipAlerts(ctx database.TenantContext, userID uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Check for newly declining relationships
	rows, err := conn.Query(ctx,
		`SELECT contact_email, contact_name, interaction_frequency
		 FROM contact_relationships
		 WHERE user_id = $1 AND is_declining = true`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("querying declining contacts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var email string
		var name *string
		var freq string
		if err := rows.Scan(&email, &name, &freq); err != nil {
			continue
		}

		displayName := email
		if name != nil && *name != "" {
			displayName = *name
		}

		// Publish declining relationship notification
		if s.rdb != nil {
			pubErr := briefings.PublishNotificationEvent(ctx, s.rdb, briefings.NotificationEvent{
				UserID:   userID.String(),
				TenantID: ctx.TenantID.String(),
				Priority: 50,
				Category: "relationship",
				Subject:  "Declining Relationship: " + displayName,
				Summary:  fmt.Sprintf("Your interaction with %s has decreased significantly. Current frequency: %s.", displayName, freq),
				Channels: []string{"email"},
			})
			if pubErr != nil {
				slog.Warn("failed to publish declining relationship alert", "error", pubErr)
			}
		}
	}

	// Check VIP contacts with response time increase > 50%
	// VIP contacts are identified via user_rules with scope containing the contact email
	vipRows, err := conn.Query(ctx,
		`SELECT cr.contact_email, cr.contact_name, cr.avg_response_time_minutes
		 FROM contact_relationships cr
		 JOIN user_rules ur ON ur.user_id = cr.user_id
		 WHERE cr.user_id = $1
		   AND ur.rule_text ILIKE '%vip%'
		   AND ur.contact_filter IS NOT NULL
		   AND cr.contact_email = ur.contact_filter
		   AND cr.avg_response_time_minutes IS NOT NULL`,
		userID,
	)
	if err != nil {
		// user_rules table might not have contact_filter, skip VIP check
		slog.Debug("VIP alert check skipped", "error", err)
		return nil
	}
	defer vipRows.Close()

	for vipRows.Next() {
		var vipEmail string
		var vipName *string
		var currentRT float64
		if err := vipRows.Scan(&vipEmail, &vipName, &currentRT); err != nil {
			continue
		}

		// Get 90-day baseline response time for this VIP contact
		var baselineRT *float64
		_ = conn.QueryRow(ctx,
			`SELECT AVG(e.response_time_minutes)
			 FROM emails e
			 WHERE e.user_id = $1
			   AND (e.from_address = $2 OR e.to_addresses->>0 = $2)
			   AND e.response_time_minutes IS NOT NULL
			   AND e.created_at >= now() - interval '90 days'
			   AND e.created_at < now() - interval '7 days'`,
			userID, vipEmail,
		).Scan(&baselineRT)

		if baselineRT == nil || *baselineRT == 0 {
			continue
		}

		increase := (currentRT - *baselineRT) / *baselineRT * 100
		if increase > 50 {
			displayName := vipEmail
			if vipName != nil && *vipName != "" {
				displayName = *vipName
			}

			if s.rdb != nil {
				_ = briefings.PublishNotificationEvent(ctx, s.rdb, briefings.NotificationEvent{
					UserID:   userID.String(),
					TenantID: ctx.TenantID.String(),
					Priority: 60,
					Category: "relationship",
					Subject:  "VIP Response Time Alert: " + displayName,
					Summary:  fmt.Sprintf("Your response time to VIP contact %s has increased %.0f%% from baseline.", displayName, increase),
					Channels: []string{"email", "push"},
				})
			}
		}
	}

	return nil
}
