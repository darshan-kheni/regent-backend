package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxFailedAttempts = 10
	LockoutDuration   = 30 * time.Minute
)

type LockoutService struct {
	pool *pgxpool.Pool
}

func NewLockoutService(pool *pgxpool.Pool) *LockoutService {
	return &LockoutService{pool: pool}
}

// CheckLocked uses context.Context (not TenantContext) deliberately.
// This is a pre-auth check — we don't know the tenant yet.
// auth_lockouts has no RLS and no tenant_id.
func (ls *LockoutService) CheckLocked(ctx context.Context, email string) (bool, error) {
	var lockedUntil *time.Time
	err := ls.pool.QueryRow(ctx,
		`SELECT locked_until FROM auth_lockouts
		 WHERE identifier = $1 AND identifier_type = 'email'
		 AND locked_until > now()`,
		email,
	).Scan(&lockedUntil)
	if err != nil {
		return false, nil // No record = not locked
	}
	return lockedUntil != nil, nil
}

func (ls *LockoutService) RecordFailure(ctx context.Context, email string) error {
	_, err := ls.pool.Exec(ctx,
		`INSERT INTO auth_lockouts (identifier, identifier_type, failed_attempts, last_attempt_at)
		 VALUES ($1, 'email', 1, now())
		 ON CONFLICT (identifier, identifier_type)
		 DO UPDATE SET
		   failed_attempts = auth_lockouts.failed_attempts + 1,
		   last_attempt_at = now(),
		   locked_until = CASE
		     WHEN auth_lockouts.failed_attempts + 1 >= $2
		     THEN now() + interval '30 minutes'
		     ELSE auth_lockouts.locked_until
		   END`,
		email, MaxFailedAttempts,
	)
	if err != nil {
		return fmt.Errorf("record failure: %w", err)
	}
	return nil
}

func (ls *LockoutService) ClearLockout(ctx context.Context, email string) error {
	_, err := ls.pool.Exec(ctx,
		`DELETE FROM auth_lockouts WHERE identifier = $1 AND identifier_type = 'email'`,
		email,
	)
	return err
}
