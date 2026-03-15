package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventType string

const (
	EventSignup          EventType = "signup"
	EventLogin           EventType = "login"
	EventLogout          EventType = "logout"
	EventLoginFailed     EventType = "login_failed"
	EventPasswordReset   EventType = "password_reset_request"
	EventPasswordChange  EventType = "password_changed"
	EventOAuthConnect    EventType = "oauth_connect"
	EventOAuthDisconnect EventType = "oauth_disconnect"
	EventSessionRevoked  EventType = "session_revoked"
	EventAccountLocked   EventType = "account_locked"
	EventAccountUnlocked EventType = "account_unlocked"
)

type AuditLogger struct {
	pool *pgxpool.Pool
}

func NewAuditLogger(pool *pgxpool.Pool) *AuditLogger {
	return &AuditLogger{pool: pool}
}

// Log uses context.Context (not TenantContext) because pre-auth events
// (login_failed for unknown users) have no tenant. Uses service role connection.
func (al *AuditLogger) Log(ctx context.Context, r *http.Request, evt EventType, userID *uuid.UUID, tenantID *uuid.UUID, provider string, success bool, meta map[string]interface{}) {
	metaJSON, _ := json.Marshal(meta)
	_, err := al.pool.Exec(ctx,
		`INSERT INTO auth_events
		 (user_id, tenant_id, event_type, provider, ip_address, user_agent, metadata, success)
		 VALUES ($1, $2, $3, $4, $5::inet, $6, $7, $8)`,
		userID, tenantID, string(evt), provider,
		r.RemoteAddr, r.UserAgent(), metaJSON, success,
	)
	if err != nil {
		slog.Error("failed to log auth event", "event", evt, "error", err)
	}
}
