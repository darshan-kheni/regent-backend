package learning

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// PreferenceSignal records a user correction or override.
type PreferenceSignal struct {
	ID             uuid.UUID       `json:"id"`
	UserID         uuid.UUID       `json:"user_id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	SignalType     string          `json:"signal_type"` // recategorize, summary_edit, reply_edit, priority_override, vip_assign
	OriginalValue  json.RawMessage `json:"original_value"`
	CorrectedValue json.RawMessage `json:"corrected_value"`
	Context        json.RawMessage `json:"context"`
	CreatedAt      time.Time       `json:"created_at"`
}

// SignalStore manages preference signals.
type SignalStore struct {
	pool *pgxpool.Pool
}

// NewSignalStore creates a new SignalStore.
func NewSignalStore(pool *pgxpool.Pool) *SignalStore {
	return &SignalStore{pool: pool}
}

// RecordSignal stores a new preference signal.
func (s *SignalStore) RecordSignal(ctx database.TenantContext, signal PreferenceSignal) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO preference_signals (user_id, tenant_id, signal_type, original_value, corrected_value, context)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		signal.UserID, ctx.TenantID, signal.SignalType, signal.OriginalValue, signal.CorrectedValue, signal.Context,
	)
	if err != nil {
		return fmt.Errorf("inserting signal: %w", err)
	}
	return nil
}

// GetRecent returns signals for a user since a given time.
func (s *SignalStore) GetRecent(ctx database.TenantContext, userID uuid.UUID, since time.Time) ([]PreferenceSignal, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, signal_type, original_value, corrected_value, context, created_at
		 FROM preference_signals
		 WHERE user_id = $1 AND created_at > $2
		 ORDER BY created_at DESC`,
		userID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("querying signals: %w", err)
	}
	defer rows.Close()

	var signals []PreferenceSignal
	for rows.Next() {
		var sig PreferenceSignal
		if err := rows.Scan(&sig.ID, &sig.UserID, &sig.TenantID, &sig.SignalType, &sig.OriginalValue, &sig.CorrectedValue, &sig.Context, &sig.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning signal: %w", err)
		}
		signals = append(signals, sig)
	}
	return signals, rows.Err()
}

// CountToday returns the number of signals for a user today.
func (s *SignalStore) CountToday(ctx database.TenantContext, userID uuid.UUID) (int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return 0, fmt.Errorf("setting RLS context: %w", err)
	}

	var count int
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM preference_signals
		 WHERE user_id = $1 AND created_at > CURRENT_DATE`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting signals: %w", err)
	}
	return count, nil
}
