package rag

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// Indexer handles background batch embedding of sent emails and corrections.
type Indexer struct {
	pool     *pgxpool.Pool
	store    *VectorStore
	embedder *Embedder
}

func NewIndexer(pool *pgxpool.Pool, store *VectorStore, embedder *Embedder) *Indexer {
	return &Indexer{pool: pool, store: store, embedder: embedder}
}

// IndexSentEmails embeds sent emails from the last 24 hours for a user.
func (idx *Indexer) IndexSentEmails(ctx database.TenantContext, userID uuid.UUID) (int, error) {
	conn, err := idx.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return 0, fmt.Errorf("setting RLS context: %w", err)
	}

	since := time.Now().Add(-24 * time.Hour)
	rows, err := conn.Query(ctx,
		`SELECT id, subject, body_text FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND created_at > $2
		   AND id NOT IN (SELECT source_id FROM embeddings WHERE user_id = $1 AND source_type = 'sent_email')`,
		userID, since,
	)
	if err != nil {
		return 0, fmt.Errorf("querying sent emails: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id uuid.UUID
		var subject, bodyText string
		if err := rows.Scan(&id, &subject, &bodyText); err != nil {
			slog.Warn("scanning sent email for indexing", "error", err)
			continue
		}

		text := fmt.Sprintf("%s %s", subject, truncate(bodyText, 500))
		embedding, err := idx.embedder.Embed(ctx, text)
		if err != nil {
			slog.Warn("embedding sent email", "email_id", id, "error", err)
			continue
		}

		rec := EmbeddingRecord{
			UserID:         userID,
			TenantID:       ctx.TenantID,
			SourceType:     "sent_email",
			SourceID:       id,
			ContentPreview: truncate(text, 200),
			Embedding:      embedding,
		}
		if err := idx.store.Insert(ctx, rec); err != nil {
			slog.Warn("inserting sent email embedding", "email_id", id, "error", err)
			continue
		}
		count++
	}

	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("iterating sent emails: %w", err)
	}

	slog.Info("indexed sent emails", "user_id", userID, "count", count)
	return count, nil
}

// IndexCorrections embeds recent preference signal corrections for a user.
// Note: preference_signals table is created in migration 023 (Wave D).
// This method will be callable once that table exists.
func (idx *Indexer) IndexCorrections(ctx database.TenantContext, userID uuid.UUID) (int, error) {
	conn, err := idx.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return 0, fmt.Errorf("setting RLS context: %w", err)
	}

	since := time.Now().Add(-24 * time.Hour)
	rows, err := conn.Query(ctx,
		`SELECT id, signal_type, context FROM preference_signals
		 WHERE user_id = $1 AND created_at > $2
		   AND id NOT IN (SELECT source_id FROM embeddings WHERE user_id = $1 AND source_type = 'correction')`,
		userID, since,
	)
	if err != nil {
		// Table may not exist yet (Wave D migration) — log and return
		slog.Debug("preference_signals table may not exist yet", "error", err)
		return 0, nil
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id uuid.UUID
		var signalType string
		var contextJSON []byte
		if err := rows.Scan(&id, &signalType, &contextJSON); err != nil {
			slog.Warn("scanning correction for indexing", "error", err)
			continue
		}

		text := fmt.Sprintf("correction:%s %s", signalType, string(contextJSON))
		embedding, err := idx.embedder.Embed(ctx, text)
		if err != nil {
			slog.Warn("embedding correction", "signal_id", id, "error", err)
			continue
		}

		rec := EmbeddingRecord{
			UserID:         userID,
			TenantID:       ctx.TenantID,
			SourceType:     "correction",
			SourceID:       id,
			ContentPreview: truncate(text, 200),
			Embedding:      embedding,
		}
		if err := idx.store.Insert(ctx, rec); err != nil {
			slog.Warn("inserting correction embedding", "signal_id", id, "error", err)
			continue
		}
		count++
	}

	slog.Info("indexed corrections", "user_id", userID, "count", count)
	return count, nil
}
