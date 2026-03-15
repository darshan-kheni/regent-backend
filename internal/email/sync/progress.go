package sync

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// SyncCursor tracks the sync progress for an account+provider combination.
type SyncCursor struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	AccountID      uuid.UUID  `json:"account_id"`
	Provider       string     `json:"provider"`
	LastUID        *int64     `json:"last_uid,omitempty"`
	LastHistoryID  *string    `json:"last_history_id,omitempty"`
	SyncState      string     `json:"sync_state"`
	ProgressPct    int        `json:"progress_pct"`
	TotalMessages  *int       `json:"total_messages,omitempty"`
	SyncedMessages int        `json:"synced_messages"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ProgressTracker manages sync_cursors table operations.
type ProgressTracker struct {
	pool *pgxpool.Pool
}

// NewProgressTracker creates a new ProgressTracker.
func NewProgressTracker(pool *pgxpool.Pool) *ProgressTracker {
	return &ProgressTracker{pool: pool}
}

// GetOrCreateCursor retrieves an existing cursor for the account+provider,
// or creates a new one in 'pending' state if none exists.
func (pt *ProgressTracker) GetOrCreateCursor(ctx database.TenantContext, accountID uuid.UUID, provider string) (*SyncCursor, error) {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting tenant context: %w", err)
	}

	// Try to find existing cursor.
	cursor := &SyncCursor{}
	err = conn.QueryRow(ctx,
		`SELECT id, tenant_id, account_id, provider, last_uid, last_history_id,
		        sync_state, progress_pct, total_messages, synced_messages,
		        error_message, started_at, completed_at, created_at, updated_at
		 FROM sync_cursors
		 WHERE account_id = $1 AND provider = $2`,
		accountID, provider,
	).Scan(
		&cursor.ID, &cursor.TenantID, &cursor.AccountID, &cursor.Provider,
		&cursor.LastUID, &cursor.LastHistoryID,
		&cursor.SyncState, &cursor.ProgressPct, &cursor.TotalMessages, &cursor.SyncedMessages,
		&cursor.ErrorMessage, &cursor.StartedAt, &cursor.CompletedAt,
		&cursor.CreatedAt, &cursor.UpdatedAt,
	)
	if err == nil {
		return cursor, nil
	}
	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("querying sync cursor: %w", err)
	}

	// Create new cursor.
	err = conn.QueryRow(ctx,
		`INSERT INTO sync_cursors (tenant_id, account_id, provider, sync_state)
		 VALUES ($1, $2, $3, 'pending')
		 RETURNING id, tenant_id, account_id, provider, last_uid, last_history_id,
		           sync_state, progress_pct, total_messages, synced_messages,
		           error_message, started_at, completed_at, created_at, updated_at`,
		ctx.TenantID, accountID, provider,
	).Scan(
		&cursor.ID, &cursor.TenantID, &cursor.AccountID, &cursor.Provider,
		&cursor.LastUID, &cursor.LastHistoryID,
		&cursor.SyncState, &cursor.ProgressPct, &cursor.TotalMessages, &cursor.SyncedMessages,
		&cursor.ErrorMessage, &cursor.StartedAt, &cursor.CompletedAt,
		&cursor.CreatedAt, &cursor.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating sync cursor: %w", err)
	}

	return cursor, nil
}

// UpdateProgress updates the sync state and progress percentage.
func (pt *ProgressTracker) UpdateProgress(ctx database.TenantContext, cursorID uuid.UUID, state string, pct int, syncedMessages int) error {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`UPDATE sync_cursors
		 SET sync_state = $1, progress_pct = $2, synced_messages = $3
		 WHERE id = $4`,
		state, pct, syncedMessages, cursorID,
	)
	if err != nil {
		return fmt.Errorf("updating progress: %w", err)
	}
	return nil
}

// UpdateLastUID updates the last processed IMAP UID on the cursor.
func (pt *ProgressTracker) UpdateLastUID(ctx database.TenantContext, cursorID uuid.UUID, uid int64) error {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`UPDATE sync_cursors SET last_uid = $1 WHERE id = $2`,
		uid, cursorID,
	)
	if err != nil {
		return fmt.Errorf("updating last_uid: %w", err)
	}
	return nil
}

// UpdateLastHistoryID updates the Gmail history ID on the cursor.
func (pt *ProgressTracker) UpdateLastHistoryID(ctx database.TenantContext, cursorID uuid.UUID, historyID string) error {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`UPDATE sync_cursors SET last_history_id = $1 WHERE id = $2`,
		historyID, cursorID,
	)
	if err != nil {
		return fmt.Errorf("updating last_history_id: %w", err)
	}
	return nil
}

// MarkStarted transitions the cursor to 'syncing' state and records the start time.
func (pt *ProgressTracker) MarkStarted(ctx database.TenantContext, cursorID uuid.UUID, totalMessages int) error {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	now := time.Now()
	_, err = conn.Exec(ctx,
		`UPDATE sync_cursors
		 SET sync_state = 'syncing', started_at = $1, total_messages = $2,
		     progress_pct = 0, synced_messages = 0, error_message = NULL
		 WHERE id = $3`,
		now, totalMessages, cursorID,
	)
	if err != nil {
		return fmt.Errorf("marking sync started: %w", err)
	}
	return nil
}

// MarkCompleted transitions the cursor to 'active' state with 100% progress.
func (pt *ProgressTracker) MarkCompleted(ctx database.TenantContext, cursorID uuid.UUID) error {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	now := time.Now()
	_, err = conn.Exec(ctx,
		`UPDATE sync_cursors
		 SET sync_state = 'active', progress_pct = 100, completed_at = $1
		 WHERE id = $2`,
		now, cursorID,
	)
	if err != nil {
		return fmt.Errorf("marking sync completed: %w", err)
	}
	return nil
}

// MarkError transitions the cursor to 'error' state with an error message.
func (pt *ProgressTracker) MarkError(ctx database.TenantContext, cursorID uuid.UUID, errMsg string) error {
	conn, err := pt.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`UPDATE sync_cursors SET sync_state = 'error', error_message = $1 WHERE id = $2`,
		errMsg, cursorID,
	)
	if err != nil {
		return fmt.Errorf("marking sync error: %w", err)
	}
	return nil
}
