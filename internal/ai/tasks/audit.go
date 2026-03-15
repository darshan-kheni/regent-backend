package tasks

import (
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// AuditWriter writes AI usage entries to the ai_audit_log table.
type AuditWriter struct {
	pool *pgxpool.Pool
}

// NewAuditWriter creates a new AuditWriter.
func NewAuditWriter(pool *pgxpool.Pool) *AuditWriter {
	return &AuditWriter{pool: pool}
}

// Log writes an audit entry with real token counts and latency.
func (w *AuditWriter) Log(ctx database.TenantContext, emailID uuid.UUID, taskType, modelUsed string, tokensIn, tokensOut int, latencyMs int64, cacheHit bool) {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO ai_audit_log (user_id, tenant_id, email_id, task_type, model_used, tokens_in, tokens_out, latency_ms, cache_hit, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())`,
		ctx.UserID, ctx.TenantID, emailID, taskType, modelUsed, tokensIn, tokensOut, latencyMs, cacheHit,
	)
	if err != nil {
		slog.Error("audit: failed to write", "email_id", emailID, "task", taskType, "error", err)
	}
}
