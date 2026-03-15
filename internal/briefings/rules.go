package briefings

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PriorityRulesEngine evaluates user-defined notification rules using
// a chain-of-responsibility pattern. First matching rule wins.
type PriorityRulesEngine struct {
	pool *pgxpool.Pool
}

// Rule represents a single user notification rule from the database.
type Rule struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TenantID  uuid.UUID
	RuleType  string          // vip, sender, keyword, category, time
	Condition json.RawMessage // type-specific JSONB
	Action    string          // critical, high, normal, suppress
	Priority  int             // evaluation order (lower = first)
	Active    bool
}

// NewPriorityRulesEngine creates a new rules engine.
func NewPriorityRulesEngine(pool *pgxpool.Pool) *PriorityRulesEngine {
	return &PriorityRulesEngine{pool: pool}
}

// Evaluate applies user notification rules to adjust the briefing's priority.
// Uses chain-of-responsibility: first matching rule wins.
// Returns the adjusted priority (0 means suppress the notification).
func (e *PriorityRulesEngine) Evaluate(ctx context.Context, b Briefing) int {
	if e.pool == nil {
		return b.Priority
	}

	rules, err := e.loadUserRules(ctx, b.UserID)
	if err != nil {
		slog.Error("rules engine: failed to load rules",
			"user_id", b.UserID, "error", err)
		return b.Priority // Fail open
	}

	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		if rule.Matches(b) {
			return actionToPriority(rule.Action)
		}
	}

	return b.Priority // No rule matched
}

// loadUserRules fetches active rules for a user, ordered by priority.
func (e *PriorityRulesEngine) loadUserRules(ctx context.Context, userID uuid.UUID) ([]Rule, error) {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, rule_type, condition, action, priority, active
		 FROM user_notification_rules
		 WHERE user_id = $1 AND active = true
		 ORDER BY priority ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.UserID, &r.TenantID, &r.RuleType,
			&r.Condition, &r.Action, &r.Priority, &r.Active); err != nil {
			continue
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// actionToPriority maps rule actions to priority scores.
func actionToPriority(action string) int {
	switch action {
	case "critical":
		return 95
	case "high":
		return 70
	case "normal":
		return 50
	case "suppress":
		return 0
	default:
		return 50
	}
}
