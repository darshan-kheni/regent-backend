package behavior

import (
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// UpdateAIUnderstandingScore computes and stores the AI Understanding Score.
// Formula: (rules*10 + patterns*5 + briefs*15 + emailsPct*0.5) capped at 100
func (s *BehaviorService) UpdateAIUnderstandingScore(ctx database.TenantContext, userID uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	var rulesCount, patternsCount, briefsCount int

	// Count user rules
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_rules WHERE user_id = $1`,
		userID,
	).Scan(&rulesCount)

	// Count learned patterns with confidence >= 70
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM learned_patterns WHERE user_id = $1 AND confidence >= 70`,
		userID,
	).Scan(&patternsCount)

	// Count context briefs
	_ = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM context_briefs WHERE user_id = $1`,
		userID,
	).Scan(&briefsCount)

	// Email processing percentage
	var emailsTotal, emailsProcessed int
	_ = conn.QueryRow(ctx,
		`SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE stage = 'complete') as processed
		 FROM email_ai_status
		 WHERE user_id = $1`,
		userID,
	).Scan(&emailsTotal, &emailsProcessed)

	emailsPct := 0.0
	if emailsTotal > 0 {
		emailsPct = float64(emailsProcessed) / float64(emailsTotal) * 100
	}

	score := float64(rulesCount)*10 + float64(patternsCount)*5 +
		float64(briefsCount)*15 + emailsPct*0.5
	if score > 100 {
		score = 100
	}
	intScore := int(math.Round(score))

	// UPSERT into behavior_profiles
	_, err = conn.Exec(ctx,
		`INSERT INTO behavior_profiles (tenant_id, user_id, ai_understanding_score, last_computed, updated_at)
		 VALUES ($1, $2, $3, now(), now())
		 ON CONFLICT (user_id) DO UPDATE SET
			ai_understanding_score = EXCLUDED.ai_understanding_score,
			last_computed = now(),
			updated_at = now()`,
		ctx.TenantID, userID, intScore,
	)
	if err != nil {
		return fmt.Errorf("upserting AI understanding score: %w", err)
	}

	return nil
}
