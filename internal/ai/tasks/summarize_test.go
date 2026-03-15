package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/darshan-kheni/regent/internal/models"
)

func TestSummaryCacheKey(t *testing.T) {
	t.Parallel()
	k1 := summaryCacheKey("hello world")
	k2 := summaryCacheKey("hello world")
	k3 := summaryCacheKey("different text")

	if k1 != k2 {
		t.Error("same input should produce same cache key")
	}
	if k1 == k3 {
		t.Error("different input should produce different cache key")
	}
	if k1 == "" {
		t.Error("cache key should not be empty")
	}
}

func TestStripQuoted(t *testing.T) {
	t.Parallel()
	input := "New content here.\n\n> This is quoted.\n> More quoted.\n\nAnother new line."
	result := stripQuoted(input)
	if result == "" {
		t.Fatal("result should not be empty")
	}
	if strings.Contains(result, "> This is quoted.") {
		t.Error("should strip quoted lines")
	}
	if !strings.Contains(result, "New content here.") {
		t.Error("should keep non-quoted lines")
	}
}

func TestStripQuoted_WriteLine(t *testing.T) {
	t.Parallel()
	input := "Hello\n\nOn Mon, Jan 1, 2026 at 10:00 AM John wrote:\n> Some old content"
	result := stripQuoted(input)
	if strings.Contains(result, "wrote:") {
		t.Error("should strip 'On ... wrote:' lines")
	}
}

func TestBuildThreadContext_EmptyOrSingle(t *testing.T) {
	t.Parallel()
	if BuildThreadContext(nil) != "" {
		t.Error("nil should return empty")
	}
	if BuildThreadContext([]models.Email{{Subject: "test"}}) != "" {
		t.Error("single email should return empty")
	}
}

func TestBuildThreadContext_MultipleEmails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	emails := []models.Email{
		{FromName: "Bob", FromAddress: "bob@test.com", BodyText: "Reply text", ReceivedAt: now},
		{FromName: "Alice", FromAddress: "alice@test.com", BodyText: "Original text", ReceivedAt: now.Add(-1 * time.Hour)},
	}
	result := BuildThreadContext(emails)
	if result == "" {
		t.Fatal("should build thread context")
	}
	// Oldest should be PREVIOUS, newest should be NEW
	if !strings.Contains(result, "[PREVIOUS]") || !strings.Contains(result, "[NEW]") {
		t.Error("should label messages as PREVIOUS and NEW")
	}
}

func TestDetectEmailType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		subject string
		want    string
	}{
		{"Re: Meeting notes", "reply"},
		{"Fwd: Check this out", "forward"},
		{"FW: Important doc", "forward"},
		{"Hello World", "original"},
	}
	for _, tt := range tests {
		email := models.Email{Subject: tt.subject}
		got := DetectEmailType(email)
		if got != tt.want {
			t.Errorf("DetectEmailType(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}
