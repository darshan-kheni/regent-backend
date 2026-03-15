package tasks

// Priority scoring is BATCHED with categorization in categorize.go.
// This file provides helper functions for priority factor adjustment.

import (
	"regexp"
	"strings"

	"github.com/darshan-kheni/regent/internal/models"
)

// AdjustPriority applies heuristic factors to the AI-generated priority score.
func AdjustPriority(email models.Email, aiScore int) (int, []string) {
	score := aiScore
	var factors []string

	// Subject keyword boost
	subjectLower := strings.ToLower(email.Subject)
	urgentKeywords := []string{"urgent", "asap", "deadline", "invoice", "due"}
	for _, kw := range urgentKeywords {
		if strings.Contains(subjectLower, kw) {
			score += 20
			factors = append(factors, "subject_keyword:"+kw)
			break // only apply once
		}
	}

	// Time sensitivity (dates within 48h)
	if hasNearDate(email.BodyText) || hasNearDate(email.Subject) {
		score += 15
		factors = append(factors, "time_sensitive")
	}

	// Cap at 100
	if score > 100 {
		score = 100
	}

	return score, factors
}

var datePatterns = regexp.MustCompile(`(?i)\b(tomorrow|today|tonight|end of day|by eod|within 24|within 48)\b`)

func hasNearDate(text string) bool {
	return datePatterns.MatchString(text)
}
