package tasks

import (
	"fmt"
	"log/slog"

	"github.com/agnivade/levenshtein"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// DedupChecker prevents duplicate task creation using two-phase dedup.
type DedupChecker struct {
	pool *pgxpool.Pool
}

// NewDedupChecker creates a new DedupChecker.
func NewDedupChecker(pool *pgxpool.Pool) *DedupChecker {
	return &DedupChecker{pool: pool}
}

// IsDuplicate returns true if a similar task already exists for this user.
// Phase 1: exact email_id match. Phase 2: Levenshtein fuzzy match within 7 days.
func (d *DedupChecker) IsDuplicate(ctx database.TenantContext, userID uuid.UUID, emailID uuid.UUID, title string) bool {
	if d.pool == nil {
		return false
	}

	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		slog.Debug("dedup: failed to acquire connection", "error", err)
		return false
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		slog.Debug("dedup: failed to set RLS", "error", err)
		return false
	}

	// Phase 1: exact email_id match
	var count int
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE user_id = $1 AND email_id = $2`,
		userID, emailID,
	).Scan(&count)
	if err != nil {
		slog.Debug("dedup: email_id check failed",
			"error", fmt.Errorf("checking email_id dedup: %w", err),
		)
		return false
	}
	if count > 0 {
		return true
	}

	// Phase 2: Levenshtein fuzzy match on active tasks from last 7 days
	rows, err := conn.Query(ctx,
		`SELECT title FROM tasks
		 WHERE user_id = $1
		   AND status NOT IN ('done', 'dismissed')
		   AND created_at > now() - interval '7 days'`,
		userID,
	)
	if err != nil {
		slog.Debug("dedup: fuzzy check query failed",
			"error", fmt.Errorf("querying recent tasks: %w", err),
		)
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			continue
		}
		distance := levenshtein.ComputeDistance(title, existing)
		maxLen := len(title)
		if len(existing) > maxLen {
			maxLen = len(existing)
		}
		if maxLen > 0 && float64(distance)/float64(maxLen) < 0.20 {
			return true
		}
	}

	return false
}
