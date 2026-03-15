package tasks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/darshan-kheni/regent/internal/models"
)

// BuildThreadContext builds a thread summary from a chain of emails.
// Orders by date, marks the newest as NEW for delta summarization.
func BuildThreadContext(emails []models.Email) string {
	if len(emails) <= 1 {
		return ""
	}

	// Sort by received_at ascending (oldest first)
	sorted := make([]models.Email, len(emails))
	copy(sorted, emails)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ReceivedAt.Before(sorted[j].ReceivedAt)
	})

	var parts []string
	for i, e := range sorted {
		label := "PREVIOUS"
		if i == len(sorted)-1 {
			label = "NEW"
		}

		body := e.BodyText
		// Strip quoted content (lines starting with >)
		body = stripQuoted(body)
		// Truncate each message in thread
		if len(body) > 300 {
			body = body[:300] + "..."
		}

		parts = append(parts, fmt.Sprintf("[%s] From: %s <%s>\n%s", label, e.FromName, e.FromAddress, body))
	}

	return strings.Join(parts, "\n---\n")
}

// stripQuoted removes quoted lines (starting with >) from email text.
func stripQuoted(text string) string {
	lines := strings.Split(text, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		// Also skip "On ... wrote:" lines
		if strings.Contains(trimmed, "wrote:") && strings.Contains(trimmed, "On ") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.Join(clean, "\n")
}

// DetectEmailType determines if an email is a forward, reply, or original.
func DetectEmailType(email models.Email) string {
	subject := strings.ToLower(email.Subject)
	if strings.HasPrefix(subject, "fwd:") || strings.HasPrefix(subject, "fw:") {
		return "forward"
	}
	if strings.HasPrefix(subject, "re:") {
		return "reply"
	}
	return "original"
}
