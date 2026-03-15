package rag

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	"github.com/darshan-kheni/regent/internal/database"
)

// EmbeddingRecord is a single row in the embeddings table.
type EmbeddingRecord struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	TenantID       uuid.UUID
	SourceType     string // "sent_email", "correction", "contact", "calendar", "task"
	SourceID       uuid.UUID
	ContentPreview string
	Embedding      []float32
	Metadata       map[string]any
}

// SearchResult is a row from match_embeddings.
type SearchResult struct {
	ID             uuid.UUID
	SourceType     string
	SourceID       uuid.UUID
	ContentPreview string
	Metadata       map[string]any
	Similarity     float64
}

// VectorStore provides CRUD for the embeddings table.
type VectorStore struct {
	pool *pgxpool.Pool
}

func NewVectorStore(pool *pgxpool.Pool) *VectorStore {
	return &VectorStore{pool: pool}
}

// Insert stores an embedding record.
func (vs *VectorStore) Insert(ctx database.TenantContext, rec EmbeddingRecord) error {
	conn, err := vs.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO embeddings (user_id, tenant_id, source_type, source_id, content_preview, embedding, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rec.UserID, rec.TenantID, rec.SourceType, rec.SourceID, rec.ContentPreview,
		pgvector.NewVector(rec.Embedding), rec.Metadata,
	)
	if err != nil {
		return fmt.Errorf("inserting embedding: %w", err)
	}
	return nil
}

// Search finds similar embeddings using the match_embeddings SQL function.
func (vs *VectorStore) Search(ctx database.TenantContext, query []float32, userID uuid.UUID, threshold float64, limit int) ([]SearchResult, error) {
	conn, err := vs.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, source_type, source_id, content_preview, metadata, similarity
		 FROM match_embeddings($1, $2, $3, $4)`,
		pgvector.NewVector(query), userID, threshold, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("searching embeddings: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.SourceType, &r.SourceID, &r.ContentPreview, &r.Metadata, &r.Similarity); err != nil {
			return nil, fmt.Errorf("scanning search result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// Delete removes all embeddings for a given source ID (for re-embedding corrections).
func (vs *VectorStore) Delete(ctx database.TenantContext, sourceID uuid.UUID) error {
	conn, err := vs.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx, `DELETE FROM embeddings WHERE source_id = $1`, sourceID)
	if err != nil {
		return fmt.Errorf("deleting embedding: %w", err)
	}
	return nil
}
