package tasks

import (
	"testing"

	"github.com/darshan-kheni/regent/internal/ai/rag"
	"github.com/darshan-kheni/regent/internal/models"
)

func TestSensitivityScorer_HighPriority(t *testing.T) {
	t.Parallel()
	s := NewSensitivityScorer()
	email := models.Email{Subject: "Normal email", BodyText: "Nothing special."}
	catResult := &CategorizeResult{PriorityScore: 90, PrimaryCategory: "Work"}
	if !s.ShouldUpgrade(email, catResult) {
		t.Error("priority > 85 should trigger upgrade")
	}
}

func TestSensitivityScorer_LegalCategory(t *testing.T) {
	t.Parallel()
	s := NewSensitivityScorer()
	email := models.Email{Subject: "Contract review", BodyText: "Please review."}
	catResult := &CategorizeResult{PriorityScore: 50, PrimaryCategory: "Legal"}
	if !s.ShouldUpgrade(email, catResult) {
		t.Error("Legal category should trigger upgrade")
	}
}

func TestSensitivityScorer_FinanceCategory(t *testing.T) {
	t.Parallel()
	s := NewSensitivityScorer()
	email := models.Email{Subject: "Invoice", BodyText: "Please pay."}
	catResult := &CategorizeResult{PriorityScore: 50, PrimaryCategory: "Finance"}
	if !s.ShouldUpgrade(email, catResult) {
		t.Error("Finance category should trigger upgrade")
	}
}

func TestSensitivityScorer_CondolenceKeywords(t *testing.T) {
	t.Parallel()
	s := NewSensitivityScorer()
	email := models.Email{Subject: "Sorry for your loss", BodyText: "My condolences."}
	catResult := &CategorizeResult{PriorityScore: 50, PrimaryCategory: "Personal"}
	if !s.ShouldUpgrade(email, catResult) {
		t.Error("condolence keywords should trigger upgrade")
	}
}

func TestSensitivityScorer_LegalTerms(t *testing.T) {
	t.Parallel()
	s := NewSensitivityScorer()
	email := models.Email{Subject: "Notice", BodyText: "Our attorney has filed a lawsuit."}
	catResult := &CategorizeResult{PriorityScore: 50, PrimaryCategory: "Work"}
	if !s.ShouldUpgrade(email, catResult) {
		t.Error("legal terms should trigger upgrade")
	}
}

func TestSensitivityScorer_NormalEmail(t *testing.T) {
	t.Parallel()
	s := NewSensitivityScorer()
	email := models.Email{Subject: "Team lunch", BodyText: "Let's grab lunch tomorrow."}
	catResult := &CategorizeResult{PriorityScore: 30, PrimaryCategory: "Personal"}
	if s.ShouldUpgrade(email, catResult) {
		t.Error("normal email should NOT trigger upgrade")
	}
}

func TestStyleExtractor_Empty(t *testing.T) {
	t.Parallel()
	s := NewStyleExtractor()
	profile := s.ExtractStyle(nil)
	if profile.Description != "" {
		t.Error("empty input should return empty description")
	}
}

func TestStyleExtractor_FormalReplies(t *testing.T) {
	t.Parallel()
	s := NewStyleExtractor()
	replies := []rag.ContextItem{
		{ContentPreview: "Dear John, Thank you for your message. Please find attached the document. Best regards, Alice"},
		{ContentPreview: "Dear Team, I sincerely appreciate your efforts. Kind regards, Alice"},
	}
	profile := s.ExtractStyle(replies)
	if profile.Formality != "formal" {
		t.Errorf("expected formal, got %s", profile.Formality)
	}
	if profile.Greeting != "Dear" {
		t.Errorf("expected greeting 'Dear', got %s", profile.Greeting)
	}
}

func TestStyleExtractor_ShortReplies(t *testing.T) {
	t.Parallel()
	s := NewStyleExtractor()
	replies := []rag.ContextItem{
		{ContentPreview: "Hi, sounds good. Thanks"},
		{ContentPreview: "Hey, let's do it. Cheers"},
	}
	profile := s.ExtractStyle(replies)
	if profile.Length != "short" {
		t.Errorf("expected short, got %s", profile.Length)
	}
}

func TestMostCommon(t *testing.T) {
	t.Parallel()
	items := []string{"Hi", "Dear", "Hi", "Hi", "Dear"}
	if result := mostCommon(items); result != "Hi" {
		t.Errorf("expected Hi, got %s", result)
	}
}
