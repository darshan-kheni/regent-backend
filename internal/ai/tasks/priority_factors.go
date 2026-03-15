package tasks

// CategoryBaseScore returns the base priority score for a category.
// Categories are AI-generated (lowercase, hyphenated). Common patterns get specific scores;
// everything else gets a sensible default based on keyword matching.
func CategoryBaseScore(category string) int {
	// Exact matches for common AI-generated categories
	scores := map[string]int{
		"urgent":      90,
		"legal":       75,
		"finance":     70,
		"work":        50,
		"security":    70,
		"health":      60,
		"travel":      40,
		"personal":    30,
		"social":      25,
		"shopping":    20,
		"newsletters": 10,
		"promotions":  10,
		"updates":     15,
		"spam":        5,
	}
	if s, ok := scores[category]; ok {
		return s
	}
	return 30 // default for unknown AI categories
}
