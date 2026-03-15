package tasks

import (
	"strings"

	"github.com/darshan-kheni/regent/internal/ai/rag"
)

// StyleProfile captures observed writing patterns from past replies.
type StyleProfile struct {
	Length      string // "short", "medium", "long"
	Formality  string // "informal", "neutral", "formal"
	Greeting   string // e.g., "Hi", "Dear", "Hey"
	SignOff    string // e.g., "Best", "Regards", "Thanks"
	Description string // Human-readable style summary for prompt injection
}

// StyleExtractor analyzes past replies to learn user writing style.
type StyleExtractor struct{}

func NewStyleExtractor() *StyleExtractor {
	return &StyleExtractor{}
}

// ExtractStyle analyzes RAG context items (past replies) to determine writing style.
func (s *StyleExtractor) ExtractStyle(pastReplies []rag.ContextItem) StyleProfile {
	if len(pastReplies) == 0 {
		return StyleProfile{}
	}

	var totalLen int
	var greetings, signoffs []string
	formalCount := 0

	for _, reply := range pastReplies {
		text := reply.ContentPreview
		totalLen += len(text)

		// Detect greeting
		if g := detectGreeting(text); g != "" {
			greetings = append(greetings, g)
		}

		// Detect sign-off
		if so := detectSignOff(text); so != "" {
			signoffs = append(signoffs, so)
		}

		// Formality heuristic
		if isFormal(text) {
			formalCount++
		}
	}

	profile := StyleProfile{}

	// Length
	avgLen := totalLen / len(pastReplies)
	switch {
	case avgLen < 100:
		profile.Length = "short"
	case avgLen < 300:
		profile.Length = "medium"
	default:
		profile.Length = "long"
	}

	// Formality
	if formalCount > len(pastReplies)/2 {
		profile.Formality = "formal"
	} else {
		profile.Formality = "neutral"
	}

	// Most common greeting/signoff
	if len(greetings) > 0 {
		profile.Greeting = mostCommon(greetings)
	}
	if len(signoffs) > 0 {
		profile.SignOff = mostCommon(signoffs)
	}

	// Build description
	profile.Description = buildStyleDescription(profile)

	return profile
}

func detectGreeting(text string) string {
	lower := strings.ToLower(text)
	greetings := []struct{ pattern, name string }{
		{"dear ", "Dear"},
		{"hi ", "Hi"},
		{"hello ", "Hello"},
		{"hey ", "Hey"},
		{"good morning", "Good morning"},
		{"good afternoon", "Good afternoon"},
	}
	for _, g := range greetings {
		if strings.HasPrefix(lower, g.pattern) || strings.Contains(lower[:min(50, len(lower))], g.pattern) {
			return g.name
		}
	}
	return ""
}

func detectSignOff(text string) string {
	lower := strings.ToLower(text)
	signoffs := []struct{ pattern, name string }{
		{"best regards", "Best regards"},
		{"kind regards", "Kind regards"},
		{"regards", "Regards"},
		{"best,", "Best"},
		{"thanks,", "Thanks"},
		{"thank you", "Thank you"},
		{"cheers", "Cheers"},
		{"sincerely", "Sincerely"},
	}
	// Check last 100 chars
	end := lower
	if len(end) > 100 {
		end = end[len(end)-100:]
	}
	for _, so := range signoffs {
		if strings.Contains(end, so.pattern) {
			return so.name
		}
	}
	return ""
}

func isFormal(text string) bool {
	lower := strings.ToLower(text)
	formalIndicators := []string{"dear ", "sincerely", "regards", "respectfully", "please find"}
	for _, ind := range formalIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	return false
}

func mostCommon(items []string) string {
	counts := make(map[string]int)
	for _, item := range items {
		counts[item]++
	}
	best := ""
	bestCount := 0
	for item, count := range counts {
		if count > bestCount {
			best = item
			bestCount = count
		}
	}
	return best
}

func buildStyleDescription(p StyleProfile) string {
	if p.Length == "" && p.Formality == "" {
		return ""
	}
	var parts []string
	if p.Length != "" {
		parts = append(parts, p.Length+" replies")
	}
	if p.Formality != "" {
		parts = append(parts, p.Formality+" tone")
	}
	if p.Greeting != "" {
		parts = append(parts, "greets with '"+p.Greeting+"'")
	}
	if p.SignOff != "" {
		parts = append(parts, "signs off with '"+p.SignOff+"'")
	}
	return "User typically writes " + strings.Join(parts, ", ")
}
