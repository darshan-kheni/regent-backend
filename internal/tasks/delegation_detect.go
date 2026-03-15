package tasks

import (
	"strings"
)

// CompletionDetector scans reply emails for task completion signals.
type CompletionDetector struct{}

// NewCompletionDetector creates a new CompletionDetector.
func NewCompletionDetector() *CompletionDetector {
	return &CompletionDetector{}
}

var strongCompleteSignals = []string{
	"done", "completed", "finished", "attached", "here you go",
	"please find attached", "as requested", "sent", "submitted",
}

var mediumCompleteSignals = []string{
	"working on it", "in progress", "almost done", "will send shortly",
	"nearly finished", "wrapping up",
}

var weakCompleteSignals = []string{
	"noted", "acknowledged", "will do", "on it", "sure", "ok",
}

// DetectCompletion analyzes a reply email body for completion signals.
func (d *CompletionDetector) DetectCompletion(body string, hasAttachment bool) CompletionResult {
	lower := strings.ToLower(body)

	strongHits := 0
	mediumHits := 0

	for _, signal := range strongCompleteSignals {
		if strings.Contains(lower, signal) {
			strongHits++
		}
	}

	for _, signal := range mediumCompleteSignals {
		if strings.Contains(lower, signal) {
			mediumHits++
		}
	}

	// Attachment + completion language = high confidence
	if hasAttachment && strongHits > 0 {
		return CompletionResult{Completed: true, Confidence: 0.95}
	}

	if strongHits >= 2 {
		return CompletionResult{Completed: true, Confidence: 0.9}
	}

	if strongHits == 1 {
		return CompletionResult{Completed: true, Confidence: 0.7, NeedsReview: true}
	}

	if mediumHits > 0 {
		return CompletionResult{InProgress: true, Confidence: 0.6, NeedsReview: true}
	}

	for _, signal := range weakCompleteSignals {
		if strings.Contains(lower, signal) {
			return CompletionResult{InProgress: true, Confidence: 0.3, NeedsReview: true}
		}
	}

	return CompletionResult{Confidence: 0}
}
