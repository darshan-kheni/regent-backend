package ai

import (
	"testing"
	"time"
)

func TestModelRouter_Route_DefaultModels(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	tests := []struct {
		name      string
		task      TaskType
		wantModel string
		wantTemp  float64
		wantMax   int
		wantTier  string
	}{
		{"categorize uses fast model", TaskCategorize, "qwen3:4b", 0.1, 150, "fast"},
		{"prioritize uses fast model", TaskPrioritize, "qwen3:4b", 0.1, 150, "fast"},
		{"summarize uses mid model", TaskSummarize, "qwen3:8b", 0.3, 300, "mid"},
		{"draft_reply uses quality model", TaskDraftReply, "gemma3:12b", 0.6, 500, "quality"},
		{"premium_draft uses premium model", TaskPremiumDraft, "gpt-oss:120b", 0.4, 800, "premium"},
		{"preference_synthesis uses premium", TaskPreferenceSynth, "gpt-oss:120b", 0.3, 1000, "premium"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := router.Route(tt.task, EmailMeta{})
			if cfg.ModelID != tt.wantModel {
				t.Errorf("ModelID = %s, want %s", cfg.ModelID, tt.wantModel)
			}
			if cfg.Temperature != tt.wantTemp {
				t.Errorf("Temperature = %f, want %f", cfg.Temperature, tt.wantTemp)
			}
			if cfg.MaxTokens != tt.wantMax {
				t.Errorf("MaxTokens = %d, want %d", cfg.MaxTokens, tt.wantMax)
			}
			if cfg.Tier != tt.wantTier {
				t.Errorf("Tier = %s, want %s", cfg.Tier, tt.wantTier)
			}
		})
	}
}

func TestModelRouter_AutoUpgrade_HighPriority(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route(TaskDraftReply, EmailMeta{Priority: 90})
	if cfg.ModelID != "gpt-oss:120b" {
		t.Errorf("high priority should upgrade to premium, got %s", cfg.ModelID)
	}
	if cfg.Tier != "premium" {
		t.Errorf("high priority should be premium tier, got %s", cfg.Tier)
	}
}

func TestModelRouter_AutoUpgrade_LegalCategory(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route(TaskDraftReply, EmailMeta{Category: "Legal"})
	if cfg.ModelID != "gpt-oss:120b" {
		t.Errorf("Legal category should upgrade to premium, got %s", cfg.ModelID)
	}
}

func TestModelRouter_AutoUpgrade_FinanceCategory(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route(TaskDraftReply, EmailMeta{Category: "Finance"})
	if cfg.ModelID != "gpt-oss:120b" {
		t.Errorf("Finance category should upgrade to premium, got %s", cfg.ModelID)
	}
}

func TestModelRouter_AutoUpgrade_VIPSender(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route(TaskDraftReply, EmailMeta{SenderIsVIP: true})
	if cfg.ModelID != "gpt-oss:120b" {
		t.Errorf("VIP sender should upgrade to premium, got %s", cfg.ModelID)
	}
}

func TestModelRouter_AutoUpgrade_UserRequested(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route(TaskDraftReply, EmailMeta{UserRequestedPremium: true})
	if cfg.ModelID != "gpt-oss:120b" {
		t.Errorf("user-requested premium should upgrade, got %s", cfg.ModelID)
	}
}

func TestModelRouter_NoUpgrade_NormalEmail(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route(TaskDraftReply, EmailMeta{Priority: 50, Category: "Work"})
	if cfg.ModelID != "gemma3:12b" {
		t.Errorf("normal email should stay at quality tier, got %s", cfg.ModelID)
	}
}

func TestModelRouter_NoUpgrade_NonDraftTask(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	// Even with high priority, categorize should NOT upgrade
	cfg := router.Route(TaskCategorize, EmailMeta{Priority: 95})
	if cfg.ModelID != "qwen3:4b" {
		t.Errorf("categorize should never upgrade, got %s", cfg.ModelID)
	}
}

func TestModelRouter_TimeoutValues(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	tests := []struct {
		task    TaskType
		timeout time.Duration
	}{
		{TaskCategorize, 5 * time.Second},
		{TaskSummarize, 8 * time.Second},
		{TaskDraftReply, 12 * time.Second},
		{TaskPremiumDraft, 20 * time.Second},
	}

	for _, tt := range tests {
		cfg := router.Route(tt.task, EmailMeta{})
		if cfg.Timeout != tt.timeout {
			t.Errorf("task %s timeout = %v, want %v", tt.task, cfg.Timeout, tt.timeout)
		}
	}
}

func TestModelRouter_UnknownTask_DefaultsToFast(t *testing.T) {
	t.Parallel()
	router := NewModelRouter()

	cfg := router.Route("unknown_task", EmailMeta{})
	if cfg.ModelID != "qwen3:4b" {
		t.Errorf("unknown task should default to categorize model, got %s", cfg.ModelID)
	}
}
