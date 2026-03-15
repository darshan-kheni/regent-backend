package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// DraftHandlers contains HTTP handlers for draft reply operations.
type DraftHandlers struct {
	pool *pgxpool.Pool
}

// NewDraftHandlers creates a new DraftHandlers instance.
func NewDraftHandlers(pool *pgxpool.Pool) *DraftHandlers {
	return &DraftHandlers{pool: pool}
}

type draftEmailInfo struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"account_id"`
	Subject     string    `json:"subject"`
	FromAddress string    `json:"from_address"`
	FromName    string    `json:"from_name"`
	BodyText    string    `json:"body_text"`
	Category    string    `json:"category,omitempty"`
	Priority    *int      `json:"priority,omitempty"`
}

type draftListResponse struct {
	ID         uuid.UUID       `json:"id"`
	EmailID    uuid.UUID       `json:"email_id"`
	Body       string          `json:"body"`
	Variant    string          `json:"variant"`
	ModelUsed  string          `json:"model_used"`
	IsPremium  bool            `json:"is_premium"`
	Confidence float64         `json:"confidence"`
	Status     string          `json:"status"`
	CreatedAt  string          `json:"created_at"`
	Email      *draftEmailInfo `json:"email,omitempty"`
}

// HandleListDrafts handles GET /api/v1/drafts?status=pending.
func (h *DraftHandlers) HandleListDrafts(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	status := r.URL.Query().Get("status")

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

	query := `SELECT d.id, d.email_id, COALESCE(d.body, ''), COALESCE(d.variant, 'standard'),
	                 COALESCE(d.model_used, ''), COALESCE(d.is_premium, false),
	                 COALESCE(d.confidence, 0), COALESCE(d.status, 'pending'), d.created_at,
	                 e.id, e.account_id, COALESCE(e.subject, ''), COALESCE(e.from_address, ''),
	                 COALESCE(e.from_name, ''), COALESCE(LEFT(e.body_text, 600), ''),
	                 COALESCE(LOWER(ec.primary_category), ''), ec.confidence
	          FROM draft_replies d
	          JOIN emails e ON e.id = d.email_id
	          LEFT JOIN LATERAL (
	              SELECT primary_category, confidence FROM email_categories
	              WHERE email_id = d.email_id ORDER BY created_at DESC LIMIT 1
	          ) ec ON true
	          WHERE d.user_id = $1`
	args := []interface{}{tc.UserID}

	if status != "" {
		query += ` AND d.status = $2`
		args = append(args, status)
	}
	query += ` ORDER BY d.created_at DESC LIMIT 50`

	rows, err := conn.Query(tc, query, args...)
	if err != nil {
		slog.Error("query drafts", "error", err)
		WriteJSON(w, r, http.StatusOK, []interface{}{})
		return
	}
	defer rows.Close()

	var drafts []draftListResponse
	for rows.Next() {
		var d draftListResponse
		var createdAt time.Time
		var ei draftEmailInfo
		var catConfidence *float64
		if err := rows.Scan(&d.ID, &d.EmailID, &d.Body, &d.Variant,
			&d.ModelUsed, &d.IsPremium, &d.Confidence, &d.Status, &createdAt,
			&ei.ID, &ei.AccountID, &ei.Subject, &ei.FromAddress,
			&ei.FromName, &ei.BodyText, &ei.Category, &catConfidence); err != nil {
			slog.Error("scan draft", "error", err)
			continue
		}
		d.CreatedAt = createdAt.Format(time.RFC3339)
		if catConfidence != nil {
			p := int(*catConfidence * 100)
			ei.Priority = &p
		}
		d.Email = &ei
		drafts = append(drafts, d)
	}

	if drafts == nil {
		drafts = []draftListResponse{}
	}

	WriteJSON(w, r, http.StatusOK, drafts)
}

// HandleApproveDraft handles POST /api/v1/drafts/{id}/approve.
func (h *DraftHandlers) HandleApproveDraft(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid draft id")
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

	tag, err := conn.Exec(tc,
		`UPDATE draft_replies SET status = 'approved' WHERE id = $1 AND user_id = $2`,
		draftID, tc.UserID)
	if err != nil {
		slog.Error("approve draft", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	if tag.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "draft not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "approved"})
}

// HandleRejectDraft handles POST /api/v1/drafts/{id}/reject.
func (h *DraftHandlers) HandleRejectDraft(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid draft id")
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

	tag, err := conn.Exec(tc,
		`DELETE FROM draft_replies WHERE id = $1 AND user_id = $2`,
		draftID, tc.UserID)
	if err != nil {
		slog.Error("reject draft", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	if tag.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "draft not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "rejected"})
}

// HandleUpdateDraft handles PUT /api/v1/drafts/{id}.
func (h *DraftHandlers) HandleUpdateDraft(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid draft id")
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
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

	tag, err := conn.Exec(tc,
		`UPDATE draft_replies SET body = $1 WHERE id = $2 AND user_id = $3`,
		body.Body, draftID, tc.UserID)
	if err != nil {
		slog.Error("update draft", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	if tag.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "draft not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleRefineDraft handles POST /api/v1/drafts/{id}/refine.
func (h *DraftHandlers) HandleRefineDraft(w http.ResponseWriter, r *http.Request) {
	// For now, just acknowledge the refine request
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}
	_ = tc

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "refine_queued"})
}
