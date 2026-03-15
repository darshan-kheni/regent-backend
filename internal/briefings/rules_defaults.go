package briefings

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateDefaultRules inserts the default notification rules for a new user.
// Currently: "Urgent" category emails get critical priority.
func CreateDefaultRules(ctx context.Context, pool *pgxpool.Pool, userID, tenantID uuid.UUID) error {
	if pool == nil {
		return nil
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO user_notification_rules (user_id, tenant_id, rule_type, condition, action, priority, active)
		 VALUES ($1, $2, 'category', '{"categories": ["Urgent"]}', 'critical', 0, true)
		 ON CONFLICT DO NOTHING`,
		userID, tenantID)
	return err
}
