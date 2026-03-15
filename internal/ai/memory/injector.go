package memory

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// MaxContextTokens is the budget for injected AI memory context.
const MaxContextTokens = 800

// ContextInjector assembles AI memory context from all 3 layers within an 800-token budget.
type ContextInjector struct {
	rules    *UserRuleStore
	briefs   *ContextBriefStore
	patterns *LearnedPatternStore
}

func NewContextInjector(rules *UserRuleStore, briefs *ContextBriefStore, patterns *LearnedPatternStore) *ContextInjector {
	return &ContextInjector{rules: rules, briefs: briefs, patterns: patterns}
}

// Inject fetches all 3 memory layers and formats them within the token budget.
func (ci *ContextInjector) Inject(ctx database.TenantContext, userID uuid.UUID, scope string, emailEmbedding []float32, keywords []string) (string, error) {
	// Layer 1: User Rules (<5ms target)
	rules, err := ci.rules.GetActive(ctx, userID, scope)
	if err != nil {
		return "", fmt.Errorf("fetching rules: %w", err)
	}

	// Layer 2: Context Briefs (<20ms target)
	briefs, err := ci.briefs.Match(ctx, userID, keywords, emailEmbedding)
	if err != nil {
		return "", fmt.Errorf("fetching briefs: %w", err)
	}

	// Layer 3: Learned Patterns (<5ms target)
	patterns, err := ci.patterns.GetConfident(ctx, userID, 70)
	if err != nil {
		return "", fmt.Errorf("fetching patterns: %w", err)
	}

	// Format each section with budget
	rulesText := formatRules(rules, 200)
	briefsText := formatBriefs(briefs, 300)
	patternsText := formatPatterns(patterns, 300)

	var sections []string
	if rulesText != "" {
		sections = append(sections, "[YOUR RULES]\n"+rulesText)
	}
	if briefsText != "" {
		sections = append(sections, "[SITUATION CONTEXT]\n"+briefsText)
	}
	if patternsText != "" {
		sections = append(sections, "[LEARNED BEHAVIOR]\n"+patternsText)
	}

	result := strings.Join(sections, "\n\n")

	// Final budget check — truncate if over
	if estimateTokens(result) > MaxContextTokens {
		maxChars := MaxContextTokens * 4
		if len(result) > maxChars {
			result = result[:maxChars]
		}
	}

	return result, nil
}

func formatRules(rules []UserRule, maxTokens int) string {
	var parts []string
	tokens := 0
	for _, r := range rules {
		line := fmt.Sprintf("- [%s] %s", r.Scope, r.Text)
		if r.ContactFilter != "" {
			line += fmt.Sprintf(" (for: %s)", r.ContactFilter)
		}
		lineTokens := estimateTokens(line)
		if tokens+lineTokens > maxTokens {
			break
		}
		parts = append(parts, line)
		tokens += lineTokens
	}
	return strings.Join(parts, "\n")
}

func formatBriefs(briefs []ContextBrief, maxTokens int) string {
	var parts []string
	tokens := 0
	for _, b := range briefs {
		line := fmt.Sprintf("- %s: %s", b.Title, b.Text)
		lineTokens := estimateTokens(line)
		if tokens+lineTokens > maxTokens {
			break
		}
		parts = append(parts, line)
		tokens += lineTokens
	}
	return strings.Join(parts, "\n")
}

func formatPatterns(patterns []LearnedPattern, maxTokens int) string {
	var parts []string
	tokens := 0
	for _, p := range patterns {
		line := fmt.Sprintf("- [%s, %d%% confident] %s", p.Category, p.Confidence, p.PatternText)
		lineTokens := estimateTokens(line)
		if tokens+lineTokens > maxTokens {
			break
		}
		parts = append(parts, line)
		tokens += lineTokens
	}
	return strings.Join(parts, "\n")
}

func estimateTokens(text string) int {
	return len(text) / 4
}
