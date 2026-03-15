package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminHandlers contains HTTP handlers for admin-level endpoints.
// These endpoints operate across tenants and require admin authorization.
type AdminHandlers struct {
	pool *pgxpool.Pool
}

// NewAdminHandlers creates a new AdminHandlers instance.
func NewAdminHandlers(pool *pgxpool.Pool) *AdminHandlers {
	return &AdminHandlers{pool: pool}
}

// ConnectionStatusResponse represents the per-user service status returned by ListConnections.
type ConnectionStatusResponse struct {
	UserID        string     `json:"user_id"`
	IMAPStatus    string     `json:"imap_status"`
	AIStatus      string     `json:"ai_status"`
	CronStatus    string     `json:"cron_status"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
}

// ListConnections returns per-user service status from user_service_status.
// GET /api/v1/admin/connections
// This is an admin-level endpoint that reads across all tenants (no RLS).
func (ah *AdminHandlers) ListConnections(w http.ResponseWriter, r *http.Request) {
	rows, err := ah.pool.Query(r.Context(),
		`SELECT user_id, imap_status, ai_status, cron_status,
		        last_heartbeat, error_message
		 FROM user_service_status
		 ORDER BY last_heartbeat DESC NULLS LAST`)
	if err != nil {
		slog.Error("admin: failed to query connection statuses", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "QUERY_FAILED", "failed to query connection statuses")
		return
	}
	defer rows.Close()

	statuses := make([]ConnectionStatusResponse, 0)
	for rows.Next() {
		var s ConnectionStatusResponse
		var heartbeat *time.Time
		var errMsg *string
		if err := rows.Scan(&s.UserID, &s.IMAPStatus, &s.AIStatus, &s.CronStatus, &heartbeat, &errMsg); err != nil {
			slog.Error("admin: failed to scan connection status row", "error", err)
			continue
		}
		s.LastHeartbeat = heartbeat
		if errMsg != nil {
			s.ErrorMessage = *errMsg
		}
		statuses = append(statuses, s)
	}

	if err := rows.Err(); err != nil {
		slog.Error("admin: error iterating connection status rows", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "QUERY_FAILED", "error reading connection statuses")
		return
	}

	WriteJSON(w, r, http.StatusOK, statuses)
}
