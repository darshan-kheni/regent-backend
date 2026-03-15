package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// SentHandlers contains HTTP handlers for sent email operations.
type SentHandlers struct {
	pool *pgxpool.Pool
}

// NewSentHandlers creates a new SentHandlers instance.
func NewSentHandlers(pool *pgxpool.Pool) *SentHandlers {
	return &SentHandlers{pool: pool}
}

// HandleListSent handles GET /api/v1/sent.
func (h *SentHandlers) HandleListSent(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	accountID := r.URL.Query().Get("account_id")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page := 1
	limit := 50
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := (page - 1) * limit

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	query := `SELECT id, account_id, COALESCE(message_id, ''), thread_id,
	                 COALESCE(from_address, ''), COALESCE(from_name, ''),
	                 COALESCE(subject, ''), COALESCE(body_text, ''),
	                 COALESCE(has_attachments, false),
	                 received_at, created_at
	          FROM emails
	          WHERE user_id = $1 AND direction = 'outbound'`
	args := []interface{}{tc.UserID}
	argN := 2

	if accountID != "" {
		if _, err := uuid.Parse(accountID); err == nil {
			query += ` AND account_id = $` + strconv.Itoa(argN)
			args = append(args, accountID)
			argN++
		}
	}

	query += ` ORDER BY received_at DESC LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	args = append(args, limit, offset)

	rows, err := conn.Query(tc, query, args...)
	if err != nil {
		slog.Error("query sent emails", "error", err)
		WriteJSON(w, r, http.StatusOK, []interface{}{})
		return
	}
	defer rows.Close()

	type sentResponse struct {
		ID             uuid.UUID  `json:"id"`
		AccountID      uuid.UUID  `json:"account_id"`
		MessageID      string     `json:"message_id"`
		ThreadID       *uuid.UUID `json:"thread_id"`
		FromAddress    string     `json:"from_address"`
		FromName       string     `json:"from_name"`
		Subject        string     `json:"subject"`
		BodyText       string     `json:"body_text"`
		HasAttachments bool       `json:"has_attachments"`
		ReceivedAt     string     `json:"received_at"`
		CreatedAt      string     `json:"created_at"`
	}

	var emails []sentResponse
	for rows.Next() {
		var e sentResponse
		var receivedAt, createdAt time.Time
		if err := rows.Scan(&e.ID, &e.AccountID, &e.MessageID, &e.ThreadID,
			&e.FromAddress, &e.FromName,
			&e.Subject, &e.BodyText, &e.HasAttachments,
			&receivedAt, &createdAt); err != nil {
			slog.Error("scan sent email", "error", err)
			continue
		}
		e.ReceivedAt = receivedAt.Format(time.RFC3339)
		e.CreatedAt = createdAt.Format(time.RFC3339)
		e.Subject = decodeMIME(e.Subject)
		e.FromName = decodeMIME(e.FromName)
		emails = append(emails, e)
	}

	if emails == nil {
		emails = []sentResponse{}
	}

	WriteJSON(w, r, http.StatusOK, emails)
}

// HandleGetSentEmail handles GET /api/v1/sent/{id}.
func (h *SentHandlers) HandleGetSentEmail(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	emailID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid email id")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	type sentDetailResponse struct {
		ID             uuid.UUID  `json:"id"`
		AccountID      uuid.UUID  `json:"account_id"`
		MessageID      string     `json:"message_id"`
		ThreadID       *uuid.UUID `json:"thread_id"`
		FromAddress    string     `json:"from_address"`
		FromName       string     `json:"from_name"`
		Subject        string     `json:"subject"`
		BodyText       string     `json:"body_text"`
		BodyHTML       *string    `json:"body_html"`
		HasAttachments bool       `json:"has_attachments"`
		ReceivedAt     string     `json:"received_at"`
		CreatedAt      string     `json:"created_at"`
	}

	var e sentDetailResponse
	var receivedAt, createdAt time.Time
	err = conn.QueryRow(tc,
		`SELECT id, account_id, COALESCE(message_id, ''), thread_id,
		        COALESCE(from_address, ''), COALESCE(from_name, ''),
		        COALESCE(subject, ''), COALESCE(body_text, ''), body_html,
		        COALESCE(has_attachments, false),
		        received_at, created_at
		 FROM emails
		 WHERE id = $1 AND user_id = $2 AND direction = 'outbound'`,
		emailID, tc.UserID).Scan(
		&e.ID, &e.AccountID, &e.MessageID, &e.ThreadID,
		&e.FromAddress, &e.FromName,
		&e.Subject, &e.BodyText, &e.BodyHTML,
		&e.HasAttachments,
		&receivedAt, &createdAt,
	)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "email not found")
		return
	}

	e.ReceivedAt = receivedAt.Format(time.RFC3339)
	e.CreatedAt = createdAt.Format(time.RFC3339)
	e.Subject = decodeMIME(e.Subject)
	e.FromName = decodeMIME(e.FromName)

	WriteJSON(w, r, http.StatusOK, e)
}

// HandleGetAdjacentSent handles GET /api/v1/sent/{id}/adjacent.
func (h *SentHandlers) HandleGetAdjacentSent(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	emailID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid email id")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	type adjacentResponse struct {
		PrevID *uuid.UUID `json:"prev_id"`
		NextID *uuid.UUID `json:"next_id"`
	}

	var resp adjacentResponse

	// Get prev (newer sent email)
	_ = conn.QueryRow(tc,
		`SELECT id FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND received_at > (SELECT received_at FROM emails WHERE id = $2)
		 ORDER BY received_at ASC LIMIT 1`,
		tc.UserID, emailID).Scan(&resp.PrevID)

	// Get next (older sent email)
	_ = conn.QueryRow(tc,
		`SELECT id FROM emails
		 WHERE user_id = $1 AND direction = 'outbound'
		   AND received_at < (SELECT received_at FROM emails WHERE id = $2)
		 ORDER BY received_at DESC LIMIT 1`,
		tc.UserID, emailID).Scan(&resp.NextID)

	WriteJSON(w, r, http.StatusOK, resp)
}
