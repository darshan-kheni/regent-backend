package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/darshan-kheni/regent/internal/behavior"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// ProductivityHandlers provides HTTP handlers for productivity metrics.
type ProductivityHandlers struct {
	svc *behavior.BehaviorService
}

// NewProductivityHandlers creates a new ProductivityHandlers instance.
func NewProductivityHandlers(svc *behavior.BehaviorService) *ProductivityHandlers {
	return &ProductivityHandlers{svc: svc}
}

// HandleProductivity returns productivity metrics.
// GET /api/v1/intelligence/productivity
func (h *ProductivityHandlers) HandleProductivity(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	metrics, err := h.svc.GetProductivityMetrics(tc, tc.UserID)
	if err != nil {
		slog.Error("behavior: failed to get productivity metrics", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get productivity metrics")
		return
	}

	WriteJSON(w, r, http.StatusOK, metrics)
}

// HandleWellnessReports returns recent wellness reports.
// GET /api/v1/intelligence/wellness-reports?limit=4
func (h *ProductivityHandlers) HandleWellnessReports(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	limit := 4
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 20 {
			limit = parsed
		}
	}

	reports, err := h.svc.GetWellnessReports(tc, tc.UserID, limit)
	if err != nil {
		slog.Error("behavior: failed to get wellness reports", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get wellness reports")
		return
	}

	WriteJSON(w, r, http.StatusOK, reports)
}
