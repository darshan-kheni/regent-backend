package api

import (
	"encoding/json"
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

	query := `SELECT e.id, e.account_id, COALESCE(e.message_id, ''), e.thread_id,
	                 COALESCE(e.from_address, ''), COALESCE(e.from_name, ''),
	                 COALESCE(e.to_addresses, '[]'::jsonb),
	                 COALESCE(e.subject, ''), COALESCE(e.body_text, ''),
	                 COALESCE(e.has_attachments, false), COALESCE(e.is_read, false),
	                 EXISTS(
	                   SELECT 1 FROM draft_replies dr
	                   JOIN emails e2 ON dr.email_id = e2.id
	                   WHERE e2.thread_id = e.thread_id AND dr.status IN ('approved', 'sent')
	                 ) as ai_drafted,
	                 e.received_at, e.created_at
	          FROM emails e
	          WHERE e.user_id = $1 AND e.direction = 'outbound'`
	args := []interface{}{tc.UserID}
	argN := 2

	if accountID != "" {
		if _, err := uuid.Parse(accountID); err == nil {
			query += ` AND e.account_id = $` + strconv.Itoa(argN)
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
		ToAddresses    []string   `json:"to_addresses"`
		Subject        string     `json:"subject"`
		BodyText       string     `json:"body_text"`
		HasAttachments bool       `json:"has_attachments"`
		IsRead         bool       `json:"is_read"`
		AiDrafted      bool       `json:"ai_drafted"`
		ReceivedAt     string     `json:"received_at"`
		CreatedAt      string     `json:"created_at"`
	}

	var emails []sentResponse
	for rows.Next() {
		var e sentResponse
		var receivedAt, createdAt time.Time
		var toJSON []byte
		if err := rows.Scan(&e.ID, &e.AccountID, &e.MessageID, &e.ThreadID,
			&e.FromAddress, &e.FromName, &toJSON,
			&e.Subject, &e.BodyText, &e.HasAttachments, &e.IsRead, &e.AiDrafted,
			&receivedAt, &createdAt); err != nil {
			slog.Error("scan sent email", "error", err)
			continue
		}
		e.ReceivedAt = receivedAt.Format(time.RFC3339)
		e.CreatedAt = createdAt.Format(time.RFC3339)
		e.Subject = decodeMIME(e.Subject)
		e.FromName = decodeMIME(e.FromName)
		_ = json.Unmarshal(toJSON, &e.ToAddresses)
		if e.ToAddresses == nil {
			e.ToAddresses = []string{}
		}
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
		ToAddresses    []string   `json:"to_addresses"`
		CcAddresses    []string   `json:"cc_addresses"`
		Subject        string     `json:"subject"`
		BodyText       string     `json:"body_text"`
		BodyHTML       *string    `json:"body_html"`
		HasAttachments bool       `json:"has_attachments"`
		ReceivedAt     string     `json:"received_at"`
		CreatedAt      string     `json:"created_at"`
	}

	var e sentDetailResponse
	var receivedAt, createdAt time.Time
	var toJSON, ccJSON []byte
	err = conn.QueryRow(tc,
		`SELECT id, account_id, COALESCE(message_id, ''), thread_id,
		        COALESCE(from_address, ''), COALESCE(from_name, ''),
		        COALESCE(to_addresses, '[]'::jsonb), COALESCE(cc_addresses, '[]'::jsonb),
		        COALESCE(subject, ''), COALESCE(body_text, ''), body_html,
		        COALESCE(has_attachments, false),
		        received_at, created_at
		 FROM emails
		 WHERE id = $1 AND user_id = $2 AND direction = 'outbound'`,
		emailID, tc.UserID).Scan(
		&e.ID, &e.AccountID, &e.MessageID, &e.ThreadID,
		&e.FromAddress, &e.FromName, &toJSON, &ccJSON,
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
	_ = json.Unmarshal(toJSON, &e.ToAddresses)
	_ = json.Unmarshal(ccJSON, &e.CcAddresses)
	if e.ToAddresses == nil {
		e.ToAddresses = []string{}
	}
	if e.CcAddresses == nil {
		e.CcAddresses = []string{}
	}

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
