package tasks

import (
	"strings"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/ai/memory"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// UserRuleEngine applies user-defined rules to override AI categorization and priority.
type UserRuleEngine struct {
	store *memory.UserRuleStore
}

// NewUserRuleEngine creates a UserRuleEngine backed by the given store.
func NewUserRuleEngine(store *memory.UserRuleStore) *UserRuleEngine {
	return &UserRuleEngine{store: store}
}

// ApplyOverrides applies user rules to modify AI categorization/priority results.
func (e *UserRuleEngine) ApplyOverrides(ctx database.TenantContext, userID uuid.UUID, email models.Email, result CategorizeResult) CategorizeResult {
	rules, err := e.store.GetActive(ctx, userID, "email")
	if err != nil {
		return result // fail open
	}

	for _, rule := range rules {
		if !matchesContact(rule, email.FromAddress) {
			continue
		}

		switch rule.Type {
		case "priority_rule":
			result = applyPriorityRule(rule, result)
		}
	}

	return result
}

func matchesContact(rule memory.UserRule, sender string) bool {
	if rule.ContactFilter == "" {
		return true // applies to all
	}
	return strings.Contains(strings.ToLower(sender), strings.ToLower(rule.ContactFilter))
}

func applyPriorityRule(rule memory.UserRule, result CategorizeResult) CategorizeResult {
	text := strings.ToLower(rule.Text)
	if strings.Contains(text, "always urgent") {
		result.PrimaryCategory = "Urgent"
		if result.PriorityScore < 90 {
			result.PriorityScore = 90
		}
		result.PriorityFactors = append(result.PriorityFactors, "user_rule_override")
	}
	if strings.Contains(text, "always high priority") {
		if result.PriorityScore < 80 {
			result.PriorityScore = 80
		}
		result.PriorityFactors = append(result.PriorityFactors, "user_rule_override")
	}
	return result
}
