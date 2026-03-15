package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// NotificationHandlers contains HTTP handlers for notification operations.
type NotificationHandlers struct {
	pool *pgxpool.Pool
}

// NewNotificationHandlers creates a new NotificationHandlers instance.
func NewNotificationHandlers(pool *pgxpool.Pool) *NotificationHandlers {
	return &NotificationHandlers{pool: pool}
}

// HandleListNotifications handles GET /api/v1/notifications.
func (h *NotificationHandlers) HandleListNotifications(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteJSON(w, r, http.StatusOK, []interface{}{})
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteJSON(w, r, http.StatusOK, []interface{}{})
		return
	}

	type notifResponse struct {
		ID        uuid.UUID `json:"id"`
		Type      string    `json:"type"`
		Title     string    `json:"title"`
		Body      string    `json:"body"`
		Channel   string    `json:"channel"`
		Status    string    `json:"status"`
		CreatedAt string    `json:"created_at"`
	}

	rows, err := conn.Query(tc,
		`SELECT id, COALESCE(event_type, 'info'), COALESCE(title, ''), COALESCE(body, ''),
		        COALESCE(channel, 'push'), COALESCE(status, 'sent'), created_at
		 FROM notification_queue
		 WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT $2`,
		tc.UserID, limit)
	if err != nil {
		// Table might not exist yet, return empty
		WriteJSON(w, r, http.StatusOK, []interface{}{})
		return
	}
	defer rows.Close()

	var notifs []notifResponse
	for rows.Next() {
		var n notifResponse
		var createdAt time.Time
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.Channel, &n.Status, &createdAt); err != nil {
			slog.Error("scan notification", "error", err)
			continue
		}
		n.CreatedAt = createdAt.Format(time.RFC3339)
		notifs = append(notifs, n)
	}

	if notifs == nil {
		notifs = []notifResponse{}
	}

	WriteJSON(w, r, http.StatusOK, notifs)
}
