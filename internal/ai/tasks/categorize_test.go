package tasks

import (
	"testing"

	"github.com/darshan-kheni/regent/internal/models"
)

func TestIsValidCategory(t *testing.T) {
	t.Parallel()
	valid := []string{"Urgent", "Work", "Finance", "Legal", "Travel", "Personal", "Newsletter", "Spam"}
	for _, c := range valid {
		if !isValidCategory(c) {
			t.Errorf("%s should be valid", c)
		}
	}
	if isValidCategory("Unknown") {
		t.Error("Unknown should not be valid")
	}
	if isValidCategory("") {
		t.Error("empty string should not be valid")
	}
}

func TestCategoryBaseScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cat   string
		score int
	}{
		{"Urgent", 90}, {"Legal", 75}, {"Finance", 70}, {"Work", 50},
		{"Newsletter", 10}, {"Spam", 5}, {"Unknown", 30},
	}
	for _, tt := range tests {
		if s := CategoryBaseScore(tt.cat); s != tt.score {
			t.Errorf("CategoryBaseScore(%s) = %d, want %d", tt.cat, s, tt.score)
		}
	}
}

func TestAdjustPriority_UrgentKeyword(t *testing.T) {
	t.Parallel()
	email := models.Email{Subject: "URGENT: Please review", BodyText: "Need your review."}
	score, factors := AdjustPriority(email, 50)
	if score < 70 {
		t.Errorf("urgent keyword should boost score, got %d", score)
	}
	found := false
	for _, f := range factors {
		if f == "subject_keyword:urgent" {
			found = true
		}
	}
	if !found {
		t.Error("should include subject_keyword factor")
	}
}

func TestAdjustPriority_TimeSensitive(t *testing.T) {
	t.Parallel()
	email := models.Email{Subject: "Meeting", BodyText: "Let's meet tomorrow at 3pm"}
	score, factors := AdjustPriority(email, 50)
	if score < 65 {
		t.Errorf("time-sensitive should boost score, got %d", score)
	}
	found := false
	for _, f := range factors {
		if f == "time_sensitive" {
			found = true
		}
	}
	if !found {
		t.Error("should include time_sensitive factor")
	}
}

func TestAdjustPriority_Cap100(t *testing.T) {
	t.Parallel()
	email := models.Email{Subject: "URGENT deadline today", BodyText: "Due today"}
	score, _ := AdjustPriority(email, 90)
	if score > 100 {
		t.Errorf("score should cap at 100, got %d", score)
	}
}

func TestHasNearDate(t *testing.T) {
	t.Parallel()
	if !hasNearDate("meeting tomorrow") {
		t.Error("should detect 'tomorrow'")
	}
	if !hasNearDate("due today") {
		t.Error("should detect 'today'")
	}
	if hasNearDate("no dates here") {
		t.Error("should not detect dates")
	}
}

func TestExtractKeywords(t *testing.T) {
	t.Parallel()
	email := models.Email{FromAddress: "test@example.com", Subject: "Important Update"}
	kw := extractKeywords(email)
	if len(kw) < 2 {
		t.Errorf("expected at least 2 keywords, got %d", len(kw))
	}
}
