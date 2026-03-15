package memory

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// LearnedPattern represents an auto-detected behavior pattern.
type LearnedPattern struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"user_id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	Category          string    `json:"category"`
	PatternText       string    `json:"pattern_text"`
	Confidence        int       `json:"confidence"`
	EvidenceCount     int       `json:"evidence_count"`
	SourceDescription string    `json:"source_description,omitempty"`
	LastUpdated       time.Time `json:"last_updated"`
	CreatedAt         time.Time `json:"created_at"`
}

// LearnedPatternStore manages learned behavior patterns.
type LearnedPatternStore struct {
	pool *pgxpool.Pool
}

func NewLearnedPatternStore(pool *pgxpool.Pool) *LearnedPatternStore {
	return &LearnedPatternStore{pool: pool}
}

// GetConfident returns patterns meeting the minimum confidence threshold.
func (s *LearnedPatternStore) GetConfident(ctx database.TenantContext, userID uuid.UUID, minConfidence int) ([]LearnedPattern, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, category, pattern_text, confidence, evidence_count, source_description, last_updated, created_at
		 FROM learned_patterns
		 WHERE user_id = $1 AND confidence >= $2
		 ORDER BY confidence DESC`,
		userID, minConfidence,
	)
	if err != nil {
		return nil, fmt.Errorf("querying patterns: %w", err)
	}
	defer rows.Close()

	var patterns []LearnedPattern
	for rows.Next() {
		var p LearnedPattern
		if err := rows.Scan(&p.ID, &p.UserID, &p.TenantID, &p.Category, &p.PatternText, &p.Confidence, &p.EvidenceCount, &p.SourceDescription, &p.LastUpdated, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning pattern: %w", err)
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

// Upsert creates or updates a learned pattern.
func (s *LearnedPatternStore) Upsert(ctx database.TenantContext, pattern LearnedPattern) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO learned_patterns (user_id, tenant_id, category, pattern_text, confidence, evidence_count, source_description)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (id) DO UPDATE SET
		   pattern_text = EXCLUDED.pattern_text,
		   confidence = EXCLUDED.confidence,
		   evidence_count = EXCLUDED.evidence_count,
		   source_description = EXCLUDED.source_description,
		   last_updated = now()`,
		pattern.UserID, ctx.TenantID, pattern.Category, pattern.PatternText, pattern.Confidence, pattern.EvidenceCount, pattern.SourceDescription,
	)
	if err != nil {
		return fmt.Errorf("upserting pattern: %w", err)
	}
	return nil
}
