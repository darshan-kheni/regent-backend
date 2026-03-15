package tasks

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

var formalityLabels = map[int]string{
	1: "very casual",
	2: "casual",
	3: "neutral",
	4: "formal",
	5: "very formal",
}

// LoadUserPersonality loads the user's AI preferences and returns a personality description
// string for prompt injection. Returns empty string if no prefs are set.
func LoadUserPersonality(ctx database.TenantContext, pool *pgxpool.Pool, userID uuid.UUID) string {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return ""
	}
	defer conn.Release()

	var formality int
	var replyStyle string
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(formality, 0), COALESCE(reply_style, '') FROM users WHERE id = $1`,
		userID,
	).Scan(&formality, &replyStyle)
	if err != nil {
		return ""
	}

	if formality == 0 && replyStyle == "" {
		return "" // No prefs set
	}

	var parts []string
	if formality > 0 {
		if label, ok := formalityLabels[formality]; ok {
			parts = append(parts, fmt.Sprintf("Formality: %s", label))
		}
	}
	if replyStyle != "" {
		parts = append(parts, fmt.Sprintf("Style: %s", replyStyle))
	}

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
