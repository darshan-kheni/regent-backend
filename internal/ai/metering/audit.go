package metering

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// AuditEntry represents a single AI operation audit record.
type AuditEntry struct {
	UserID        uuid.UUID
	TenantID      uuid.UUID
	EmailID       *uuid.UUID // nil for non-email tasks
	TaskType      string
	ModelUsed     string
	ModelVersion  string
	PromptVersion string
	InputHash     string
	TokensIn      int
	TokensOut     int
	Confidence    float64
	LatencyMs     int
	CacheHit      bool
	RawOutput     json.RawMessage
	ParsedOutput  json.RawMessage
}

// AuditLogger logs every AI call to the ai_audit_log table.
type AuditLogger struct {
	pool *pgxpool.Pool
}

// NewAuditLogger creates a new AuditLogger backed by the given connection pool.
func NewAuditLogger(pool *pgxpool.Pool) *AuditLogger {
	return &AuditLogger{pool: pool}
}

// Log writes an audit entry. MUST be called BEFORE returning the AI result.
func (al *AuditLogger) Log(ctx database.TenantContext, entry AuditEntry) error {
	conn, err := al.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection for audit: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS for audit: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO ai_audit_log (user_id, tenant_id, email_id, task_type, model_used, model_version,
		  prompt_version, input_hash, tokens_in, tokens_out, confidence, latency_ms, cache_hit, raw_output, parsed_output)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		entry.UserID, entry.TenantID, entry.EmailID, entry.TaskType, entry.ModelUsed, entry.ModelVersion,
		entry.PromptVersion, entry.InputHash, entry.TokensIn, entry.TokensOut, entry.Confidence,
		entry.LatencyMs, entry.CacheHit, entry.RawOutput, entry.ParsedOutput,
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}
	return nil
}
