package behavior

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// Formal and informal keyword indicators for formality scoring.
var (
	formalIndicators = []string{
		"dear", "sincerely", "regards", "please find attached",
		"respectfully", "cordially", "to whom it may concern",
		"per our conversation", "as discussed",
	}
	informalIndicators = []string{
		"hey", "thanks!", "yo", "lol", "haha", "cheers",
		"sup", "gonna", "wanna", "btw", "fyi",
	}
)

// ComputeFormality calculates formality distribution for emails in a date range.
// Returns: {"formal": 60, "neutral": 30, "casual": 10}
func (s *BehaviorService) ComputeFormality(ctx database.TenantContext, userID uuid.UUID, dayStart, dayEnd time.Time) (map[string]float64, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT body_text, tone_classification
		 FROM emails
		 WHERE user_id = $1
		   AND direction = 'outbound'
		   AND created_at >= $2 AND created_at < $3`,
		userID, dayStart, dayEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("querying emails for formality: %w", err)
	}
	defer rows.Close()

	var formalCount, neutralCount, casualCount int
	for rows.Next() {
		var bodyText string
		var tone *string
		if err := rows.Scan(&bodyText, &tone); err != nil {
			return nil, fmt.Errorf("scanning formality row: %w", err)
		}

		score := scoreFormalityForEmail(bodyText, tone)
		switch {
		case score >= 60:
			formalCount++
		case score <= 40:
			casualCount++
		default:
			neutralCount++
		}
	}

	total := formalCount + neutralCount + casualCount
	dist := map[string]float64{"formal": 0, "neutral": 0, "casual": 0}
	if total > 0 {
		dist["formal"] = float64(formalCount) / float64(total) * 100.0
		dist["neutral"] = float64(neutralCount) / float64(total) * 100.0
		dist["casual"] = float64(casualCount) / float64(total) * 100.0
	}
	return dist, nil
}

// scoreFormalityForEmail scores a single email 0-100 (0=casual, 100=formal).
func scoreFormalityForEmail(bodyText string, tone *string) int {
	score := 50 // neutral baseline
	lower := strings.ToLower(bodyText)

	// Keyword scoring
	for _, kw := range formalIndicators {
		if strings.Contains(lower, kw) {
			score += 5
		}
	}
	for _, kw := range informalIndicators {
		if strings.Contains(lower, kw) {
			score -= 5
		}
	}

	// Tone-based adjustment
	if tone != nil {
		switch *tone {
		case "formal_legal":
			score += 30
		case "professional":
			score += 10
		case "casual":
			score -= 30
		case "warm_friendly":
			score -= 10
		case "urgent":
			score += 5
		}
	}

	// Clamp to 0-100
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}
