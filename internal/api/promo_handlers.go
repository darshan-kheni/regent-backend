package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/billing"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// PromoHandlers contains HTTP handlers for promo code operations.
type PromoHandlers struct {
	promo *billing.PromoService
}

// NewPromoHandlers creates a new PromoHandlers.
func NewPromoHandlers(promo *billing.PromoService) *PromoHandlers {
	return &PromoHandlers{promo: promo}
}

type validatePromoRequest struct {
	Code string `json:"code"`
}

// HandleValidatePromo checks whether a promo code is valid for the current user.
// POST /api/v1/billing/promo/validate
func (h *PromoHandlers) HandleValidatePromo(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	var req validatePromoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if req.Code == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_CODE", "promo code is required")
		return
	}

	result, err := h.promo.Validate(tc, req.Code, tc.UserID)
	if err != nil {
		slog.Error("promo: validation error", "error", err, "tenant_id", tc.TenantID)
		WriteError(w, r, http.StatusInternalServerError, "PROMO_ERROR", "failed to validate promo code")
		return
	}

	WriteJSON(w, r, http.StatusOK, result)
}

type applyPromoRequest struct {
	Code string `json:"code"`
}

// HandleApplyPromo validates and applies a promo code for the current user.
// POST /api/v1/billing/promo/apply
func (h *PromoHandlers) HandleApplyPromo(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	var req applyPromoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if req.Code == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_CODE", "promo code is required")
		return
	}

	if err := h.promo.Apply(tc, req.Code, tc.UserID); err != nil {
		slog.Error("promo: apply error", "error", err, "tenant_id", tc.TenantID)
		WriteError(w, r, http.StatusBadRequest, "PROMO_FAILED", err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status":  "applied",
		"message": "Promo code applied successfully",
	})
}

type createPromoRequest struct {
	Code            string     `json:"code"`
	Type            string     `json:"type"`
	DiscountPercent *int       `json:"discount_percent,omitempty"`
	TrialDays       *int       `json:"trial_days,omitempty"`
	Plan            string     `json:"plan"`
	MaxUses         *int       `json:"max_uses,omitempty"`
	ValidFrom       *time.Time `json:"valid_from,omitempty"`
	ValidUntil      *time.Time `json:"valid_until,omitempty"`
}

// HandleCreatePromo creates a new promo code (admin only).
// POST /api/v1/admin/promo
func (h *PromoHandlers) HandleCreatePromo(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	var req createPromoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if req.Code == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_CODE", "code is required")
		return
	}
	if req.Type != "discount" && req.Type != "trial" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_TYPE", "type must be 'discount' or 'trial'")
		return
	}
	if req.Plan == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_PLAN", "plan is required")
		return
	}
	if _, found := billing.GetPlanByName(req.Plan); !found {
		WriteError(w, r, http.StatusBadRequest, "INVALID_PLAN", "unknown plan name")
		return
	}
	if req.Type == "discount" && (req.DiscountPercent == nil || *req.DiscountPercent < 1 || *req.DiscountPercent > 100) {
		WriteError(w, r, http.StatusBadRequest, "INVALID_DISCOUNT", "discount_percent must be between 1 and 100")
		return
	}
	if req.Type == "trial" && (req.TrialDays == nil || *req.TrialDays < 1 || *req.TrialDays > 365) {
		WriteError(w, r, http.StatusBadRequest, "INVALID_TRIAL", "trial_days must be between 1 and 365")
		return
	}

	pc := billing.PromoCode{
		Code:            req.Code,
		Type:            req.Type,
		DiscountPercent: req.DiscountPercent,
		TrialDays:       req.TrialDays,
		Plan:            req.Plan,
		MaxUses:         req.MaxUses,
		CreatedBy:       &tc.UserID,
	}
	if req.ValidFrom != nil {
		pc.ValidFrom = *req.ValidFrom
	}
	if req.ValidUntil != nil {
		pc.ValidUntil = req.ValidUntil
	}

	created, err := h.promo.Create(r.Context(), pc)
	if err != nil {
		slog.Error("promo: create error", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "CREATE_FAILED", "failed to create promo code")
		return
	}

	WriteJSON(w, r, http.StatusCreated, created)
}

// HandleListPromos lists all promo codes (admin only).
// GET /api/v1/admin/promo
func (h *PromoHandlers) HandleListPromos(w http.ResponseWriter, r *http.Request) {
	codes, err := h.promo.ListAll(r.Context())
	if err != nil {
		slog.Error("promo: list error", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "QUERY_FAILED", "failed to list promo codes")
		return
	}

	WriteJSON(w, r, http.StatusOK, codes)
}

// HandleDeactivatePromo deactivates a promo code (admin only).
// PATCH /api/v1/admin/promo/{id}
func (h *PromoHandlers) HandleDeactivatePromo(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	codeID, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "invalid promo code ID")
		return
	}

	if err := h.promo.Deactivate(r.Context(), codeID); err != nil {
		slog.Error("promo: deactivate error", "error", err, "code_id", codeID)
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status": "deactivated",
	})
}
