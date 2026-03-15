package behavior

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// ContactRelationship represents a computed contact relationship.
type ContactRelationship struct {
	ContactEmail         string     `json:"contact_email"`
	ContactName          *string    `json:"contact_name"`
	InteractionCount     int        `json:"interaction_count"`
	AvgResponseTimeMin   *float64   `json:"avg_response_time_minutes"`
	DominantTone         *string    `json:"dominant_tone"`
	SentimentTrend       string     `json:"sentiment_trend"`
	InteractionFrequency string     `json:"interaction_frequency"`
	LastInteraction      *time.Time `json:"last_interaction"`
	FirstInteraction     *time.Time `json:"first_interaction"`
	IsDeclining          bool       `json:"is_declining"`
}

// frequencyLevels ordered from most to least frequent for decline detection.
var frequencyLevels = []string{"Daily", "3x/week", "Weekly", "Bi-weekly", "Monthly"}

// ComputeContactRelationships scores all contacts for a user.
func (s *BehaviorService) ComputeContactRelationships(ctx database.TenantContext, userID uuid.UUID, date time.Time) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Get interaction stats per contact
	rows, err := conn.Query(ctx,
		`SELECT
			CASE WHEN direction = 'outbound' THEN to_addresses->>0
			     ELSE from_address END as contact_email,
			COUNT(*) as interaction_count,
			AVG(response_time_minutes) FILTER (WHERE response_time_minutes IS NOT NULL) as avg_rt,
			MODE() WITHIN GROUP (ORDER BY tone_classification) FILTER (WHERE tone_classification IS NOT NULL) as dominant_tone,
			MAX(created_at) as last_interaction,
			MIN(created_at) as first_interaction
		 FROM emails
		 WHERE user_id = $1
		   AND created_at >= $2::date - interval '90 days'
		 GROUP BY contact_email
		 HAVING COUNT(*) >= 2`,
		userID, date.Format("2006-01-02"),
	)
	if err != nil {
		return fmt.Errorf("querying contact interactions: %w", err)
	}
	defer rows.Close()

	type contactData struct {
		email            string
		interactionCount int
		avgRT            *float64
		dominantTone     *string
		lastInteraction  *time.Time
		firstInteraction *time.Time
	}

	var contacts []contactData
	for rows.Next() {
		var c contactData
		if err := rows.Scan(&c.email, &c.interactionCount, &c.avgRT, &c.dominantTone, &c.lastInteraction, &c.firstInteraction); err != nil {
			slog.Warn("failed to scan contact row", "error", err)
			continue
		}
		if c.email == "" {
			continue
		}
		contacts = append(contacts, c)
	}

	for _, c := range contacts {
		// Classify frequency
		freq := classifyFrequency(c.firstInteraction, c.lastInteraction, c.interactionCount)

		// Compute sentiment trend (this month vs last month)
		trend := s.computeSentimentTrend(ctx, conn, userID, c.email, date)

		// Check for declining (compare to previous frequency stored in DB)
		isDeclining := s.checkDeclining(ctx, conn, userID, c.email, freq)

		// Get contact name (best effort from most recent email)
		var contactName *string
		_ = conn.QueryRow(ctx,
			`SELECT from_name FROM emails
			 WHERE user_id = $1 AND from_address = $2 AND from_name != ''
			 ORDER BY created_at DESC LIMIT 1`,
			userID, c.email,
		).Scan(&contactName)

		// UPSERT
		_, err := conn.Exec(ctx,
			`INSERT INTO contact_relationships (
				tenant_id, user_id, contact_email, contact_name,
				interaction_count, avg_response_time_minutes, dominant_tone,
				sentiment_trend, interaction_frequency, last_interaction, first_interaction,
				is_declining, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
			ON CONFLICT (user_id, contact_email) DO UPDATE SET
				contact_name = COALESCE(EXCLUDED.contact_name, contact_relationships.contact_name),
				interaction_count = EXCLUDED.interaction_count,
				avg_response_time_minutes = EXCLUDED.avg_response_time_minutes,
				dominant_tone = EXCLUDED.dominant_tone,
				sentiment_trend = EXCLUDED.sentiment_trend,
				interaction_frequency = EXCLUDED.interaction_frequency,
				last_interaction = EXCLUDED.last_interaction,
				first_interaction = EXCLUDED.first_interaction,
				is_declining = EXCLUDED.is_declining,
				updated_at = now()`,
			ctx.TenantID, userID, c.email, contactName,
			c.interactionCount, c.avgRT, c.dominantTone,
			trend, freq, c.lastInteraction, c.firstInteraction,
			isDeclining,
		)
		if err != nil {
			slog.Warn("failed to upsert contact relationship",
				"contact", c.email, "error", err)
		}
	}

	return nil
}

// classifyFrequency determines interaction frequency based on average gap between interactions.
func classifyFrequency(first, last *time.Time, count int) string {
	if first == nil || last == nil || count < 2 {
		return "Monthly"
	}
	totalDays := last.Sub(*first).Hours() / 24
	if totalDays < 1 {
		totalDays = 1
	}
	avgGapDays := totalDays / float64(count-1)

	switch {
	case avgGapDays < 1:
		return "Daily"
	case avgGapDays < 3:
		return "3x/week"
	case avgGapDays < 7:
		return "Weekly"
	case avgGapDays < 14:
		return "Bi-weekly"
	default:
		return "Monthly"
	}
}

// computeSentimentTrend compares this month's interaction count vs last month.
// Up if +20%, down if -20%, stable otherwise.
func (s *BehaviorService) computeSentimentTrend(ctx database.TenantContext, conn *pgxpool.Conn, userID uuid.UUID, contactEmail string, date time.Time) string {
	thisMonthStart := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	lastMonthStart := thisMonthStart.AddDate(0, -1, 0)

	var thisMonth, lastMonth int
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails
		 WHERE user_id = $1
		   AND (from_address = $2 OR to_addresses->>0 = $2)
		   AND created_at >= $3 AND created_at < $4`,
		userID, contactEmail, thisMonthStart.Format("2006-01-02"), date.AddDate(0, 0, 1).Format("2006-01-02"),
	).Scan(&thisMonth)

	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails
		 WHERE user_id = $1
		   AND (from_address = $2 OR to_addresses->>0 = $2)
		   AND created_at >= $3 AND created_at < $4`,
		userID, contactEmail, lastMonthStart.Format("2006-01-02"), thisMonthStart.Format("2006-01-02"),
	).Scan(&lastMonth)

	if lastMonth == 0 {
		return "stable"
	}

	changePct := float64(thisMonth-lastMonth) / float64(lastMonth) * 100
	switch {
	case changePct > 20:
		return "up"
	case changePct < -20:
		return "down"
	default:
		return "stable"
	}
}

// checkDeclining compares new frequency to previously stored frequency.
// Declining if frequency drops 2+ levels (e.g., Daily -> Weekly).
func (s *BehaviorService) checkDeclining(ctx database.TenantContext, conn *pgxpool.Conn, userID uuid.UUID, contactEmail, newFreq string) bool {
	var oldFreq *string
	_ = conn.QueryRow(ctx,
		`SELECT interaction_frequency FROM contact_relationships
		 WHERE user_id = $1 AND contact_email = $2`,
		userID, contactEmail,
	).Scan(&oldFreq)

	if oldFreq == nil || *oldFreq == "" {
		return false
	}

	oldLevel := frequencyLevel(*oldFreq)
	newLevel := frequencyLevel(newFreq)

	// Declining if new level is 2+ positions lower (higher index = less frequent)
	return newLevel-oldLevel >= 2
}

// frequencyLevel returns the ordinal position of a frequency (0=Daily, 4=Monthly).
func frequencyLevel(freq string) int {
	for i, f := range frequencyLevels {
		if f == freq {
			return i
		}
	}
	return 4 // default to Monthly
}
