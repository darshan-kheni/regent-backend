package memory

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// PlanRuleLimits defines max rules per plan. 0 = unlimited.
var PlanRuleLimits = map[string]int{
	"free": 10, "attache": 25, "privy_council": 50, "estate": 0,
}

// UserRule represents a user-defined AI instruction.
type UserRule struct {
	ID            uuid.UUID `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	Scope         string    `json:"scope"`
	Type          string    `json:"type"`
	Text          string    `json:"text"`
	ContactFilter string    `json:"contact_filter,omitempty"`
	Active        bool      `json:"active"`
	Priority      int       `json:"priority"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// UserRuleUpdate holds optional update fields.
type UserRuleUpdate struct {
	Text          *string `json:"text,omitempty"`
	Active        *bool   `json:"active,omitempty"`
	Priority      *int    `json:"priority,omitempty"`
	ContactFilter *string `json:"contact_filter,omitempty"`
}

// UserRuleStore provides CRUD operations for user rules with plan-gated limits.
type UserRuleStore struct {
	pool *pgxpool.Pool
}

func NewUserRuleStore(pool *pgxpool.Pool) *UserRuleStore {
	return &UserRuleStore{pool: pool}
}

// Create inserts a new user rule, enforcing plan limits.
func (s *UserRuleStore) Create(ctx database.TenantContext, rule UserRule, plan string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Check plan limit
	limit, ok := PlanRuleLimits[plan]
	if !ok {
		limit = 10 // default to free
	}
	if limit > 0 {
		var count int
		err := conn.QueryRow(ctx,
			`SELECT COUNT(*) FROM user_rules WHERE user_id = $1 AND active = true`,
			rule.UserID,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("counting rules: %w", err)
		}
		if count >= limit {
			return fmt.Errorf("rule limit reached for plan %s (max %d)", plan, limit)
		}
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO user_rules (user_id, tenant_id, scope, type, text, contact_filter, active, priority)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rule.UserID, ctx.TenantID, rule.Scope, rule.Type, rule.Text, rule.ContactFilter, rule.Active, rule.Priority,
	)
	if err != nil {
		return fmt.Errorf("inserting rule: %w", err)
	}
	return nil
}

// List returns all rules for a user, optionally filtered by scope.
func (s *UserRuleStore) List(ctx database.TenantContext, userID uuid.UUID, scope string) ([]UserRule, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	query := `SELECT id, user_id, tenant_id, scope, type, text, contact_filter, active, priority, created_at, updated_at
		FROM user_rules WHERE user_id = $1`
	args := []any{userID}
	if scope != "" {
		query += ` AND scope = $2`
		args = append(args, scope)
	}
	query += ` ORDER BY priority DESC, created_at`

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying rules: %w", err)
	}
	defer rows.Close()

	return scanRules(rows)
}

// GetActive returns active rules for a user and scope (including 'all' scope).
func (s *UserRuleStore) GetActive(ctx database.TenantContext, userID uuid.UUID, scope string) ([]UserRule, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, scope, type, text, contact_filter, active, priority, created_at, updated_at
		 FROM user_rules
		 WHERE user_id = $1 AND scope IN ($2, 'all') AND active = true
		 ORDER BY priority DESC`,
		userID, scope,
	)
	if err != nil {
		return nil, fmt.Errorf("querying active rules: %w", err)
	}
	defer rows.Close()

	return scanRules(rows)
}

// Update modifies a rule by ID.
func (s *UserRuleStore) Update(ctx database.TenantContext, id uuid.UUID, updates UserRuleUpdate) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Build dynamic UPDATE
	setClauses := []string{"updated_at = now()"}
	args := []any{}
	argNum := 1

	if updates.Text != nil {
		setClauses = append(setClauses, fmt.Sprintf("text = $%d", argNum))
		args = append(args, *updates.Text)
		argNum++
	}
	if updates.Active != nil {
		setClauses = append(setClauses, fmt.Sprintf("active = $%d", argNum))
		args = append(args, *updates.Active)
		argNum++
	}
	if updates.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argNum))
		args = append(args, *updates.Priority)
		argNum++
	}
	if updates.ContactFilter != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_filter = $%d", argNum))
		args = append(args, *updates.ContactFilter)
		argNum++
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE user_rules SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argNum)

	_, err = conn.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating rule: %w", err)
	}
	return nil
}

// Delete removes a rule by ID.
func (s *UserRuleStore) Delete(ctx database.TenantContext, id uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx, `DELETE FROM user_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting rule: %w", err)
	}
	return nil
}

func scanRules(rows pgx.Rows) ([]UserRule, error) {
	var rules []UserRule
	for rows.Next() {
		var r UserRule
		if err := rows.Scan(&r.ID, &r.UserID, &r.TenantID, &r.Scope, &r.Type, &r.Text, &r.ContactFilter, &r.Active, &r.Priority, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
