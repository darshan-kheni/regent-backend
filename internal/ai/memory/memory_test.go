package memory

import (
	"strings"
	"testing"
)

func TestPlanRuleLimits(t *testing.T) {
	t.Parallel()
	if PlanRuleLimits["free"] != 10 {
		t.Errorf("free limit = %d, want 10", PlanRuleLimits["free"])
	}
	if PlanRuleLimits["attache"] != 25 {
		t.Errorf("attache limit = %d, want 25", PlanRuleLimits["attache"])
	}
	if PlanRuleLimits["privy_council"] != 50 {
		t.Errorf("privy limit = %d, want 50", PlanRuleLimits["privy_council"])
	}
	if PlanRuleLimits["estate"] != 0 {
		t.Errorf("estate limit = %d, want 0 (unlimited)", PlanRuleLimits["estate"])
	}
}

func TestFormatRules_WithinBudget(t *testing.T) {
	t.Parallel()
	rules := []UserRule{
		{Scope: "email", Text: "Always reply formally", Priority: 1},
		{Scope: "all", Text: "Use British English", Priority: 0},
	}
	result := formatRules(rules, 200)
	if result == "" {
		t.Error("expected non-empty rules text")
	}
	if estimateTokens(result) > 200 {
		t.Errorf("rules text exceeds budget: %d tokens", estimateTokens(result))
	}
}

func TestFormatRules_TruncatesAtBudget(t *testing.T) {
	t.Parallel()
	var rules []UserRule
	for i := 0; i < 50; i++ {
		rules = append(rules, UserRule{
			Scope: "email",
			Text:  "This is a rule with some reasonable text that takes up tokens in the prompt budget.",
		})
	}
	result := formatRules(rules, 50) // Very small budget
	tokens := estimateTokens(result)
	if tokens > 50 {
		t.Errorf("rules should be truncated to ~50 tokens, got %d", tokens)
	}
}

func TestFormatRules_WithContactFilter(t *testing.T) {
	t.Parallel()
	rules := []UserRule{
		{Scope: "email", Text: "Be extra formal", ContactFilter: "ceo@company.com"},
	}
	result := formatRules(rules, 200)
	if !strings.Contains(result, "(for: ceo@company.com)") {
		t.Error("expected contact filter in output")
	}
}

func TestFormatBriefs_TruncatesAtBudget(t *testing.T) {
	t.Parallel()
	var briefs []ContextBrief
	for i := 0; i < 20; i++ {
		briefs = append(briefs, ContextBrief{
			Title: "Important Context",
			Text:  "This is a context brief with detailed information about an ongoing situation.",
		})
	}
	result := formatBriefs(briefs, 100)
	if estimateTokens(result) > 100 {
		t.Errorf("briefs should be truncated to ~100 tokens, got %d", estimateTokens(result))
	}
}

func TestFormatPatterns_IncludesConfidence(t *testing.T) {
	t.Parallel()
	patterns := []LearnedPattern{
		{Category: "communication_style", PatternText: "Prefers formal tone", Confidence: 85},
		{Category: "reply_patterns", PatternText: "Usually responds within 1 hour", Confidence: 72},
	}
	result := formatPatterns(patterns, 300)
	if result == "" {
		t.Error("expected non-empty patterns text")
	}
	if !strings.Contains(result, "85%") || !strings.Contains(result, "72%") {
		t.Error("expected both patterns to be included")
	}
}

func TestFormatPatterns_TruncatesAtBudget(t *testing.T) {
	t.Parallel()
	var patterns []LearnedPattern
	for i := 0; i < 30; i++ {
		patterns = append(patterns, LearnedPattern{
			Category:    "communication_style",
			PatternText: "A long pattern description that consumes tokens from the budget allocation.",
			Confidence:  80,
		})
	}
	result := formatPatterns(patterns, 80)
	if estimateTokens(result) > 80 {
		t.Errorf("patterns should be truncated to ~80 tokens, got %d", estimateTokens(result))
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	if estimateTokens("") != 0 {
		t.Error("empty string should be 0 tokens")
	}
	if estimateTokens("test") != 1 {
		t.Error("4 chars should be ~1 token")
	}
	if estimateTokens("hello world testing") != 4 {
		t.Errorf("19 chars should be ~4 tokens, got %d", estimateTokens("hello world testing"))
	}
}

func TestMaxContextTokensConstant(t *testing.T) {
	t.Parallel()
	if MaxContextTokens != 800 {
		t.Errorf("MaxContextTokens = %d, want 800", MaxContextTokens)
	}
}

func TestFormatRules_EmptyInput(t *testing.T) {
	t.Parallel()
	result := formatRules(nil, 200)
	if result != "" {
		t.Errorf("expected empty string for nil rules, got %q", result)
	}
}

func TestFormatBriefs_EmptyInput(t *testing.T) {
	t.Parallel()
	result := formatBriefs(nil, 300)
	if result != "" {
		t.Errorf("expected empty string for nil briefs, got %q", result)
	}
}

func TestFormatPatterns_EmptyInput(t *testing.T) {
	t.Parallel()
	result := formatPatterns(nil, 300)
	if result != "" {
		t.Errorf("expected empty string for nil patterns, got %q", result)
	}
}
