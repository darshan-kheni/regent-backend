package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthReporter periodically writes heartbeats to the user_service_status table.
// It tracks per-service statuses (imap, ai, cron, briefing) and reports them
// to the database at a configurable interval.
//
// Heartbeat writes bypass RLS since they are an admin-level operation that must
// work cross-tenant. Direct pool.Acquire is used without SetRLSContext.
type HealthReporter struct {
	userID   uuid.UUID
	tenantID uuid.UUID
	pool     *pgxpool.Pool
	interval time.Duration

	mu       sync.Mutex
	statuses map[string]string
	errMsg   string
}

// NewHealthReporter creates a HealthReporter with all services initially "stopped".
func NewHealthReporter(userID, tenantID uuid.UUID, pool *pgxpool.Pool, interval time.Duration) *HealthReporter {
	return &HealthReporter{
		userID:   userID,
		tenantID: tenantID,
		pool:     pool,
		interval: interval,
		statuses: map[string]string{
			"imap":     "stopped",
			"ai":       "stopped",
			"cron":     "stopped",
			"briefing": "stopped",
		},
	}
}

// Run starts the heartbeat ticker loop. It blocks until ctx is cancelled.
func (h *HealthReporter) Run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Send an initial heartbeat immediately on start.
	h.reportHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.reportHeartbeat(ctx)
		}
	}
}

// SetStatus updates the status of a named service in a thread-safe manner.
// Valid service names: "imap", "ai", "cron", "briefing".
// Valid statuses: "stopped", "starting", "running", "error", "paused".
func (h *HealthReporter) SetStatus(service, status string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statuses[service] = status
}

// SetError sets a service to "error" status with an error message.
func (h *HealthReporter) SetError(service, errMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statuses[service] = "error"
	h.errMsg = errMsg
}

// getSnapshot returns a thread-safe copy of current statuses and error message.
func (h *HealthReporter) getSnapshot() (map[string]string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make(map[string]string, len(h.statuses))
	for k, v := range h.statuses {
		cp[k] = v
	}
	return cp, h.errMsg
}

// reportHeartbeat upserts the current statuses into user_service_status.
// Uses ON CONFLICT (user_id) DO UPDATE to be idempotent.
// Bypasses RLS — this is an admin-level write.
func (h *HealthReporter) reportHeartbeat(ctx context.Context) {
	if h.pool == nil {
		return
	}

	statuses, errMsg := h.getSnapshot()

	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		slog.Error("health reporter: failed to acquire connection",
			"user_id", h.userID,
			"error", err,
		)
		return
	}
	defer conn.Release()

	query := `
		INSERT INTO user_service_status (tenant_id, user_id, imap_status, ai_status, cron_status, briefing_status, last_heartbeat, error_message, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), $7, now())
		ON CONFLICT (user_id) DO UPDATE SET
			imap_status     = EXCLUDED.imap_status,
			ai_status       = EXCLUDED.ai_status,
			cron_status     = EXCLUDED.cron_status,
			briefing_status = EXCLUDED.briefing_status,
			last_heartbeat  = now(),
			error_message   = EXCLUDED.error_message
	`

	_, err = conn.Exec(ctx, query,
		h.tenantID,
		h.userID,
		statuses["imap"],
		statuses["ai"],
		statuses["cron"],
		statuses["briefing"],
		errMsg,
	)
	if err != nil {
		slog.Error("health reporter: failed to write heartbeat",
			"user_id", h.userID,
			"error", fmt.Errorf("writing heartbeat: %w", err),
		)
	}
}
