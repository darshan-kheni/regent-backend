package api

import (
	"encoding/json"
	"log/slog"
	gomime "mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// decodeMIME decodes MIME encoded-words in a string (e.g. =?UTF-8?B?...?= or =?UTF-8?Q?...?=).
var mimeDecoder = &gomime.WordDecoder{}

func decodeMIME(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	decoded, err := mimeDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

// EmailHandlers contains HTTP handlers for email operations.
type EmailHandlers struct {
	pool *pgxpool.Pool
}

// NewEmailHandlers creates a new EmailHandlers instance.
func NewEmailHandlers(pool *pgxpool.Pool) *EmailHandlers {
	return &EmailHandlers{pool: pool}
}

// HandleListEmails handles GET /api/v1/emails.
func (h *EmailHandlers) HandleListEmails(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	// Parse query params
	accountID := r.URL.Query().Get("account_id")
	category := r.URL.Query().Get("category")
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

	// Build query dynamically based on filters
	query := `SELECT e.id, e.account_id, COALESCE(e.message_id, ''), e.thread_id,
	                 COALESCE(e.direction, 'inbound'),
	                 COALESCE(e.from_address, ''), COALESCE(e.from_name, ''),
	                 COALESCE(e.subject, ''), COALESCE(e.body_text, ''),
	                 COALESCE(e.has_attachments, false),
	                 e.received_at, COALESCE(e.is_read, false), COALESCE(e.is_starred, false),
	                 e.created_at,
	                 COALESCE(LOWER(ec.primary_category), 'uncategorized'), COALESCE(ec.confidence, 0),
	                 COALESCE(es.headline, ''), COALESCE(es.action_required, false),
	                 COALESCE(e.tone_classification, '')
	          FROM emails e
	          LEFT JOIN LATERAL (SELECT primary_category, confidence FROM email_categories WHERE email_id = e.id ORDER BY created_at DESC LIMIT 1) ec ON true
	          LEFT JOIN LATERAL (SELECT headline, action_required FROM email_summaries WHERE email_id = e.id ORDER BY created_at DESC LIMIT 1) es ON true
	          WHERE e.user_id = $1`
	args := []interface{}{tc.UserID}
	argN := 2

	if accountID != "" {
		if _, err := uuid.Parse(accountID); err == nil {
			query += ` AND e.account_id = $` + strconv.Itoa(argN)
			args = append(args, accountID)
			argN++
		}
	}
	if category != "" && category != "all" {
		query += ` AND LOWER(ec.primary_category) = $` + strconv.Itoa(argN)
		args = append(args, strings.ToLower(category))
		argN++
	}

	// Count total matching emails for pagination
	countQuery := `SELECT COUNT(*) FROM emails e
	          LEFT JOIN LATERAL (SELECT primary_category FROM email_categories WHERE email_id = e.id ORDER BY created_at DESC LIMIT 1) ec ON true
	          WHERE e.user_id = $1`
	countArgs := []interface{}{tc.UserID}
	countArgN := 2
	if accountID != "" {
		if _, parseErr := uuid.Parse(accountID); parseErr == nil {
			countQuery += ` AND e.account_id = $` + strconv.Itoa(countArgN)
			countArgs = append(countArgs, accountID)
			countArgN++
		}
	}
	if category != "" && category != "all" {
		countQuery += ` AND LOWER(ec.primary_category) = $` + strconv.Itoa(countArgN)
		countArgs = append(countArgs, strings.ToLower(category))
	}
	var totalCount int
	_ = conn.QueryRow(tc, countQuery, countArgs...).Scan(&totalCount)

	query += ` ORDER BY e.received_at DESC LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	args = append(args, limit, offset)

	rows, err := conn.Query(tc, query, args...)
	if err != nil {
		slog.Error("query emails", "error", err)
		WriteJSON(w, r, http.StatusOK, map[string]interface{}{"emails": []interface{}{}, "total": 0})
		return
	}
	defer rows.Close()

	type emailResponse struct {
		ID             uuid.UUID  `json:"id"`
		AccountID      uuid.UUID  `json:"account_id"`
		MessageID      string     `json:"message_id"`
		ThreadID       *uuid.UUID `json:"thread_id"`
		Direction      string     `json:"direction"`
		FromAddress    string     `json:"from_address"`
		FromName       string     `json:"from_name"`
		Subject        string     `json:"subject"`
		BodyText       string     `json:"body_text"`
		HasAttachments bool       `json:"has_attachments"`
		ReceivedAt     string     `json:"received_at"`
		IsRead         bool       `json:"is_read"`
		IsStarred      bool       `json:"is_starred"`
		CreatedAt      string     `json:"created_at"`
		Category       string     `json:"category"`
		Confidence     float64    `json:"confidence"`
		Summary        string     `json:"summary"`
		ActionRequired bool       `json:"action_required"`
		Tone           string     `json:"tone,omitempty"`
	}

	var emails []emailResponse
	for rows.Next() {
		var e emailResponse
		var receivedAt, createdAt time.Time
		if err := rows.Scan(&e.ID, &e.AccountID, &e.MessageID, &e.ThreadID,
			&e.Direction, &e.FromAddress, &e.FromName,
			&e.Subject, &e.BodyText, &e.HasAttachments,
			&receivedAt, &e.IsRead, &e.IsStarred, &createdAt,
			&e.Category, &e.Confidence,
			&e.Summary, &e.ActionRequired, &e.Tone); err != nil {
			slog.Error("scan email", "error", err)
			continue
		}
		e.ReceivedAt = receivedAt.Format(time.RFC3339)
		e.CreatedAt = createdAt.Format(time.RFC3339)
		e.Subject = decodeMIME(e.Subject)
		e.FromName = decodeMIME(e.FromName)
		// Normalize category to lowercase for frontend
		e.Category = strings.ToLower(e.Category)
		emails = append(emails, e)
	}

	if emails == nil {
		emails = []emailResponse{}
	}

	// Category counts: always fetch for ALL categories (not filtered by current category).
	// Only filtered by account if one is selected. This ensures tab counts stay stable.
	catCountQuery := `SELECT COALESCE(LOWER(ec.primary_category), 'uncategorized') as cat, COUNT(*)
		FROM emails e
		LEFT JOIN LATERAL (SELECT primary_category FROM email_categories WHERE email_id = e.id ORDER BY created_at DESC LIMIT 1) ec ON true
		WHERE e.user_id = $1`
	catCountArgs := []interface{}{tc.UserID}
	catCountArgN := 2
	if accountID != "" {
		if _, parseErr := uuid.Parse(accountID); parseErr == nil {
			catCountQuery += ` AND e.account_id = $` + strconv.Itoa(catCountArgN)
			catCountArgs = append(catCountArgs, accountID)
		}
	}
	catCountQuery += ` GROUP BY cat`

	categoryCounts := map[string]int{}
	allCount := 0
	catRows, catErr := conn.Query(tc, catCountQuery, catCountArgs...)
	if catErr == nil {
		defer catRows.Close()
		for catRows.Next() {
			var cat string
			var cnt int
			if catRows.Scan(&cat, &cnt) == nil {
				categoryCounts[cat] += cnt
				allCount += cnt
			}
		}
	}
	categoryCounts["all"] = allCount

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"emails":          emails,
		"total":           totalCount,
		"page":            page,
		"limit":           limit,
		"category_counts": categoryCounts,
	})
}

// HandleGetEmail handles GET /api/v1/emails/{id}.
func (h *EmailHandlers) HandleGetEmail(w http.ResponseWriter, r *http.Request) {
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

	type emailDetailResponse struct {
		ID             uuid.UUID       `json:"id"`
		AccountID      uuid.UUID       `json:"account_id"`
		MessageID      string          `json:"message_id"`
		ThreadID       *uuid.UUID      `json:"thread_id"`
		Direction      string          `json:"direction"`
		FromAddress    string          `json:"from_address"`
		FromName       string          `json:"from_name"`
		ToAddresses    json.RawMessage `json:"to_addresses"`
		CcAddresses    json.RawMessage `json:"cc_addresses"`
		Subject        string          `json:"subject"`
		BodyText       string          `json:"body_text"`
		BodyHTML       *string         `json:"body_html"`
		HasAttachments bool            `json:"has_attachments"`
		Attachments    json.RawMessage `json:"attachments"`
		ReceivedAt     string          `json:"received_at"`
		IsRead         bool            `json:"is_read"`
		IsStarred      bool            `json:"is_starred"`
		CreatedAt      string          `json:"created_at"`
		Category       string          `json:"category"`
		Confidence     float64         `json:"confidence"`
		Summary        *string         `json:"summary"`
		Priority       *int            `json:"priority"`
	}

	var e emailDetailResponse
	var receivedAt, createdAt time.Time
	err = conn.QueryRow(tc,
		`SELECT e.id, e.account_id, COALESCE(e.message_id, ''), e.thread_id,
		        COALESCE(e.direction, 'inbound'),
		        COALESCE(e.from_address, ''), COALESCE(e.from_name, ''),
		        COALESCE(e.to_addresses, '[]'::jsonb), COALESCE(e.cc_addresses, '[]'::jsonb),
		        COALESCE(e.subject, ''), COALESCE(e.body_text, ''), e.body_html,
		        COALESCE(e.has_attachments, false), COALESCE(e.attachments, '[]'::jsonb),
		        e.received_at, COALESCE(e.is_read, false), COALESCE(e.is_starred, false),
		        e.created_at,
		        COALESCE(ec.primary_category, 'uncategorized'), COALESCE(ec.confidence, 0),
		        es.summary
		 FROM emails e
		 LEFT JOIN email_categories ec ON ec.email_id = e.id
		 LEFT JOIN email_summaries es ON es.email_id = e.id
		 WHERE e.id = $1 AND e.user_id = $2`,
		emailID, tc.UserID).Scan(
		&e.ID, &e.AccountID, &e.MessageID, &e.ThreadID,
		&e.Direction, &e.FromAddress, &e.FromName,
		&e.ToAddresses, &e.CcAddresses,
		&e.Subject, &e.BodyText, &e.BodyHTML,
		&e.HasAttachments, &e.Attachments,
		&receivedAt, &e.IsRead, &e.IsStarred, &createdAt,
		&e.Category, &e.Confidence,
		&e.Summary,
	)
	if err != nil {
		slog.Error("get email", "error", err, "email_id", emailID)
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "email not found")
		return
	}

	e.ReceivedAt = receivedAt.Format(time.RFC3339)
	e.CreatedAt = createdAt.Format(time.RFC3339)
	e.Subject = decodeMIME(e.Subject)
	e.FromName = decodeMIME(e.FromName)

	// Auto-mark as read when opened (like Gmail/Outlook)
	if !e.IsRead {
		_, _ = conn.Exec(tc, `UPDATE emails SET is_read = true WHERE id = $1 AND user_id = $2`, emailID, tc.UserID)
		e.IsRead = true
	}

	WriteJSON(w, r, http.StatusOK, e)
}

// HandleGetDraft handles GET /api/v1/emails/{id}/draft.
func (h *EmailHandlers) HandleGetDraft(w http.ResponseWriter, r *http.Request) {
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

	type draftResponse struct {
		ID         uuid.UUID `json:"id"`
		EmailID    uuid.UUID `json:"email_id"`
		Body       string    `json:"body"`
		Variant    string    `json:"variant"`
		ModelUsed  string    `json:"model_used"`
		IsPremium  bool      `json:"is_premium"`
		Confidence float64   `json:"confidence"`
		Status     string    `json:"status"`
		CreatedAt  string    `json:"created_at"`
	}

	var d draftResponse
	var createdAt time.Time
	err = conn.QueryRow(tc,
		`SELECT id, email_id, COALESCE(body, ''), COALESCE(variant, 'standard'),
		        COALESCE(model_used, ''), COALESCE(is_premium, false),
		        COALESCE(confidence, 0), COALESCE(status, 'pending'), created_at
		 FROM draft_replies
		 WHERE email_id = $1 AND user_id = $2
		 ORDER BY created_at DESC LIMIT 1`,
		emailID, tc.UserID).Scan(
		&d.ID, &d.EmailID, &d.Body, &d.Variant,
		&d.ModelUsed, &d.IsPremium, &d.Confidence, &d.Status, &createdAt,
	)
	if err != nil {
		// No draft yet — return null instead of 404 so the frontend handles gracefully
		WriteJSON(w, r, http.StatusOK, nil)
		return
	}

	d.CreatedAt = createdAt.Format(time.RFC3339)
	WriteJSON(w, r, http.StatusOK, d)
}

// HandleListSummaries handles GET /api/v1/summaries.
// Query params: account_id, category, date (YYYY-MM-DD), from (YYYY-MM-DD), to (YYYY-MM-DD), page, limit
func (h *EmailHandlers) HandleListSummaries(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	accountID := r.URL.Query().Get("account_id")
	category := r.URL.Query().Get("category")
	dateStr := r.URL.Query().Get("date")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
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
		slog.Error("acquire connection for summaries", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls for summaries", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	query := `SELECT e.id, e.account_id, COALESCE(e.from_address, ''), COALESCE(e.from_name, ''),
	                 COALESCE(e.subject, ''), COALESCE(e.has_attachments, false),
	                 e.received_at,
	                 COALESCE(LOWER(ec.primary_category), 'uncategorized'), COALESCE(ec.confidence, 0),
	                 es.headline, COALESCE(es.action_required, false),
	                 COALESCE(e.tone_classification, '')
	          FROM emails e
	          INNER JOIN email_summaries es ON es.email_id = e.id
	          LEFT JOIN LATERAL (SELECT primary_category, confidence FROM email_categories WHERE email_id = e.id ORDER BY created_at DESC LIMIT 1) ec ON true
	          WHERE e.user_id = $1`
	args := []interface{}{tc.UserID}
	argN := 2

	if accountID != "" {
		if _, err := uuid.Parse(accountID); err == nil {
			query += ` AND e.account_id = $` + strconv.Itoa(argN)
			args = append(args, accountID)
			argN++
		}
	}
	if category != "" && category != "all" {
		query += ` AND LOWER(ec.primary_category) = $` + strconv.Itoa(argN)
		args = append(args, strings.ToLower(category))
		argN++
	}

	// Date filtering
	if dateStr != "" {
		if d, err := time.Parse("2006-01-02", dateStr); err == nil {
			query += ` AND e.received_at >= $` + strconv.Itoa(argN) + ` AND e.received_at < $` + strconv.Itoa(argN+1)
			args = append(args, d, d.AddDate(0, 0, 1))
			argN += 2
		}
	} else {
		if fromStr != "" {
			if d, err := time.Parse("2006-01-02", fromStr); err == nil {
				query += ` AND e.received_at >= $` + strconv.Itoa(argN)
				args = append(args, d)
				argN++
			}
		}
		if toStr != "" {
			if d, err := time.Parse("2006-01-02", toStr); err == nil {
				query += ` AND e.received_at < $` + strconv.Itoa(argN)
				args = append(args, d.AddDate(0, 0, 1))
				argN++
			}
		}
	}

	// Count
	countQuery := strings.Replace(query, `SELECT e.id, e.account_id, COALESCE(e.from_address, ''), COALESCE(e.from_name, ''),
	                 COALESCE(e.subject, ''), COALESCE(e.has_attachments, false),
	                 e.received_at,
	                 COALESCE(LOWER(ec.primary_category), 'uncategorized'), COALESCE(ec.confidence, 0),
	                 es.headline, COALESCE(es.action_required, false),
	                 COALESCE(e.tone_classification, '')`, "SELECT COUNT(*)", 1)
	var totalCount int
	_ = conn.QueryRow(tc, countQuery, args...).Scan(&totalCount)

	query += ` ORDER BY e.received_at DESC LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	args = append(args, limit, offset)

	rows, err := conn.Query(tc, query, args...)
	if err != nil {
		slog.Error("query summaries", "error", err)
		WriteJSON(w, r, http.StatusOK, map[string]interface{}{"summaries": []interface{}{}, "total": 0})
		return
	}
	defer rows.Close()

	type summaryResponse struct {
		EmailID        uuid.UUID `json:"email_id"`
		AccountID      uuid.UUID `json:"account_id"`
		FromAddress    string    `json:"from_address"`
		FromName       string    `json:"from_name"`
		Subject        string    `json:"subject"`
		HasAttachments bool      `json:"has_attachments"`
		ReceivedAt     string    `json:"received_at"`
		Category       string    `json:"category"`
		Confidence     float64   `json:"confidence"`
		Headline       string    `json:"headline"`
		ActionRequired bool      `json:"action_required"`
		Tone           string    `json:"tone,omitempty"`
	}

	var summaries []summaryResponse
	for rows.Next() {
		var s summaryResponse
		var receivedAt time.Time
		if err := rows.Scan(&s.EmailID, &s.AccountID, &s.FromAddress, &s.FromName,
			&s.Subject, &s.HasAttachments, &receivedAt,
			&s.Category, &s.Confidence,
			&s.Headline, &s.ActionRequired, &s.Tone); err != nil {
			slog.Error("scan summary", "error", err)
			continue
		}
		s.ReceivedAt = receivedAt.Format(time.RFC3339)
		s.Subject = decodeMIME(s.Subject)
		s.FromName = decodeMIME(s.FromName)
		s.Category = strings.ToLower(s.Category)
		summaries = append(summaries, s)
	}

	if summaries == nil {
		summaries = []summaryResponse{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"summaries": summaries,
		"total":     totalCount,
		"page":      page,
		"limit":     limit,
	})
}
