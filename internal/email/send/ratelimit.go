package send

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// PlanRateLimits defines sends-per-hour for each billing plan.
// A value of 0 means unlimited.
var PlanRateLimits = map[string]int{
	"free":          10,
	"attache":       30,
	"privy_council": 100,
	"estate":        0,
}

// SendRateLimiter enforces plan-based sending rate limits.
type SendRateLimiter struct {
	pool *pgxpool.Pool
}

// NewSendRateLimiter creates a new rate limiter backed by the email_send_log table.
func NewSendRateLimiter(pool *pgxpool.Pool) *SendRateLimiter {
	return &SendRateLimiter{pool: pool}
}

// CheckSendLimit verifies the user has not exceeded their plan's hourly send limit.
// Returns nil if within limits, or an error if the limit is exceeded.
func (rl *SendRateLimiter) CheckSendLimit(ctx database.TenantContext, userID uuid.UUID, plan string) error {
	limit, ok := PlanRateLimits[plan]
	if !ok {
		return fmt.Errorf("unknown plan: %s", plan)
	}
	if limit == 0 {
		return nil // unlimited
	}

	conn, err := rl.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	var count int
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM email_send_log
		 WHERE user_id = $1 AND status = 'sent' AND sent_at > now() - interval '1 hour'`,
		userID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking send count: %w", err)
	}

	if count >= limit {
		return fmt.Errorf("send rate limit exceeded: %d/%d per hour", count, limit)
	}
	return nil
}
