package memory

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	"github.com/darshan-kheni/regent/internal/database"
)

// ContextBrief represents a situational context piece for the AI.
type ContextBrief struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	Title     string     `json:"title"`
	Scope     string     `json:"scope"`
	Text      string     `json:"text"`
	Keywords  []string   `json:"keywords"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ContextBriefStore manages context briefs with keyword + semantic matching.
type ContextBriefStore struct {
	pool *pgxpool.Pool
}

func NewContextBriefStore(pool *pgxpool.Pool) *ContextBriefStore {
	return &ContextBriefStore{pool: pool}
}

// Create inserts a new context brief.
func (s *ContextBriefStore) Create(ctx database.TenantContext, brief ContextBrief, embedding []float32) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	keywordsJSON, err := json.Marshal(brief.Keywords)
	if err != nil {
		return fmt.Errorf("marshaling keywords: %w", err)
	}

	var vec *pgvector.Vector
	if len(embedding) > 0 {
		v := pgvector.NewVector(embedding)
		vec = &v
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO context_briefs (user_id, tenant_id, title, scope, text, keywords, expires_at, embedding)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		brief.UserID, ctx.TenantID, brief.Title, brief.Scope, brief.Text, keywordsJSON, brief.ExpiresAt, vec,
	)
	if err != nil {
		return fmt.Errorf("inserting brief: %w", err)
	}
	return nil
}

// Match finds active context briefs using keyword overlap and/or semantic similarity.
func (s *ContextBriefStore) Match(ctx database.TenantContext, userID uuid.UUID, keywords []string, queryEmbedding []float32) ([]ContextBrief, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	// Active filter: expires_at IS NULL OR expires_at > now()
	// Match by keyword overlap OR semantic similarity
	// Pass keywords as []string directly — pgx encodes it as TEXT[] for the ?| operator.

	var rows pgx.Rows
	if len(queryEmbedding) > 0 && len(keywords) > 0 {
		rows, err = conn.Query(ctx,
			`SELECT id, user_id, tenant_id, title, scope, text, keywords, expires_at, created_at
			 FROM context_briefs
			 WHERE user_id = $1
			   AND (expires_at IS NULL OR expires_at > now())
			   AND (keywords ?| $2::text[] OR (embedding IS NOT NULL AND 1 - (embedding <=> $3) > 0.5))
			 ORDER BY created_at DESC
			 LIMIT 5`,
			userID, keywords, pgvector.NewVector(queryEmbedding),
		)
	} else if len(keywords) > 0 {
		rows, err = conn.Query(ctx,
			`SELECT id, user_id, tenant_id, title, scope, text, keywords, expires_at, created_at
			 FROM context_briefs
			 WHERE user_id = $1
			   AND (expires_at IS NULL OR expires_at > now())
			   AND keywords ?| $2::text[]
			 ORDER BY created_at DESC
			 LIMIT 5`,
			userID, keywords,
		)
	} else if len(queryEmbedding) > 0 {
		rows, err = conn.Query(ctx,
			`SELECT id, user_id, tenant_id, title, scope, text, keywords, expires_at, created_at
			 FROM context_briefs
			 WHERE user_id = $1
			   AND (expires_at IS NULL OR expires_at > now())
			   AND embedding IS NOT NULL
			   AND 1 - (embedding <=> $2) > 0.5
			 ORDER BY embedding <=> $2
			 LIMIT 5`,
			userID, pgvector.NewVector(queryEmbedding),
		)
	} else {
		rows, err = conn.Query(ctx,
			`SELECT id, user_id, tenant_id, title, scope, text, keywords, expires_at, created_at
			 FROM context_briefs
			 WHERE user_id = $1
			   AND (expires_at IS NULL OR expires_at > now())
			 ORDER BY created_at DESC
			 LIMIT 5`,
			userID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("querying briefs: %w", err)
	}
	defer rows.Close()

	var briefs []ContextBrief
	for rows.Next() {
		var b ContextBrief
		var kw []byte
		if err := rows.Scan(&b.ID, &b.UserID, &b.TenantID, &b.Title, &b.Scope, &b.Text, &kw, &b.ExpiresAt, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning brief: %w", err)
		}
		_ = json.Unmarshal(kw, &b.Keywords)
		briefs = append(briefs, b)
	}
	return briefs, rows.Err()
}

// Delete removes a context brief by ID.
func (s *ContextBriefStore) Delete(ctx database.TenantContext, id uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx, `DELETE FROM context_briefs WHERE id = $1`, id)
	return err
}
