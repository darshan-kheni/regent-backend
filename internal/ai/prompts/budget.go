package prompts

import "sort"

const (
	BudgetSystem  = 400
	BudgetContext = 300
	BudgetFewShot = 200
)

// PromptSection represents a named section of the prompt with budget constraints.
type PromptSection struct {
	Name      string
	Content   string
	Priority  int // Higher = more important, never truncate 4
	MaxTokens int
}

// EstimateTokens gives a rough token count (chars / 4).
func EstimateTokens(text string) int {
	return len(text) / 4
}

// EnforceBudget truncates lowest-priority sections to fit within token limits.
// Truncation order: few_shot first, then context. System and input are never truncated.
func EnforceBudget(sections []PromptSection) []PromptSection {
	// Sort by priority ascending (lowest first = truncated first)
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	totalTokens := 0
	for i := range sections {
		tokens := EstimateTokens(sections[i].Content)
		if sections[i].MaxTokens > 0 && tokens > sections[i].MaxTokens {
			// Truncate to fit budget
			maxChars := sections[i].MaxTokens * 4
			if len(sections[i].Content) > maxChars {
				sections[i].Content = sections[i].Content[:maxChars]
			}
			tokens = sections[i].MaxTokens
		}
		totalTokens += tokens
	}

	// Re-sort by original position for assembly (system, context, few_shot, input)
	orderMap := map[string]int{"system": 0, "memory": 1, "context": 2, "few_shot": 3, "input": 4}
	sort.Slice(sections, func(i, j int) bool {
		return orderMap[sections[i].Name] < orderMap[sections[j].Name]
	})

	return sections
}
