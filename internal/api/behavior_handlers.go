package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/behavior"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// BehaviorHandlers provides HTTP handlers for behavior intelligence endpoints.
type BehaviorHandlers struct {
	svc  *behavior.BehaviorService
	pool *pgxpool.Pool
}

// NewBehaviorHandlers creates a new BehaviorHandlers instance.
func NewBehaviorHandlers(svc *behavior.BehaviorService, pool *pgxpool.Pool) *BehaviorHandlers {
	return &BehaviorHandlers{svc: svc, pool: pool}
}

// HandleOverview returns behavior profile, latest stress indicators, and quick stats.
// GET /api/v1/intelligence/overview
func (h *BehaviorHandlers) HandleOverview(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	overview, err := h.svc.GetOverview(tc, tc.UserID)
	if err != nil {
		slog.Error("behavior: failed to get overview", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get overview")
		return
	}

	WriteJSON(w, r, http.StatusOK, overview)
}

// HandleCommunication returns communication metrics for a given period.
// GET /api/v1/intelligence/communication?period=daily|weekly|monthly
func (h *BehaviorHandlers) HandleCommunication(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "daily"
	}
	if period != "daily" && period != "weekly" && period != "monthly" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_PARAM", "period must be daily, weekly, or monthly")
		return
	}

	metrics, err := h.svc.GetCommunicationMetrics(tc, tc.UserID, period)
	if err != nil {
		slog.Error("behavior: failed to get communication metrics", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get communication metrics")
		return
	}

	WriteJSON(w, r, http.StatusOK, metrics)
}

// HandleWLB returns WLB score, penalties, and trend data.
// GET /api/v1/intelligence/wlb
func (h *BehaviorHandlers) HandleWLB(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	wlb, err := h.svc.GetWLBData(tc, tc.UserID)
	if err != nil {
		slog.Error("behavior: failed to get WLB data", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get WLB data")
		return
	}

	WriteJSON(w, r, http.StatusOK, wlb)
}

// HandleStress returns current stress indicators with status colors.
// GET /api/v1/intelligence/stress
func (h *BehaviorHandlers) HandleStress(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	indicators, err := h.svc.GetStressIndicators(tc, tc.UserID)
	if err != nil {
		slog.Error("behavior: failed to get stress indicators", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get stress indicators")
		return
	}

	WriteJSON(w, r, http.StatusOK, indicators)
}

// HandleUpdateCalibration updates user WLB calibration preferences.
// PUT /api/v1/settings/behavior
func (h *BehaviorHandlers) HandleUpdateCalibration(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	var cal behavior.UserCalibration
	if err := json.NewDecoder(r.Body).Decode(&cal); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if err := h.svc.SaveCalibration(tc, tc.UserID, cal); err != nil {
		slog.Error("behavior: failed to save calibration", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save calibration")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleComputeNow triggers immediate behavior intelligence computation.
// POST /api/v1/intelligence/compute
func (h *BehaviorHandlers) HandleComputeNow(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	// Use user's configured timezone for date calculations
	var userTZ string
	conn, connErr := h.pool.Acquire(tc)
	if connErr == nil {
		_ = conn.QueryRow(tc, `SELECT COALESCE(timezone, 'UTC') FROM users WHERE id = $1`, tc.UserID).Scan(&userTZ)
		conn.Release()
	}
	if userTZ == "" {
		userTZ = "UTC"
	}
	loc, locErr := time.LoadLocation(userTZ)
	if locErr != nil {
		loc = time.UTC
	}
	yesterday := time.Now().In(loc).AddDate(0, 0, -1)

	slog.Info("behavior: manual compute triggered", "user_id", tc.UserID)

	err := h.svc.RunNightly(tc, tc.UserID, yesterday)
	if err != nil {
		slog.Warn("behavior: manual compute completed with errors", "user_id", tc.UserID, "error", err)
		// Still return 200 since partial results are useful
		WriteJSON(w, r, http.StatusOK, map[string]interface{}{
			"status":  "partial",
			"message": "Computation completed with some errors: " + err.Error(),
		})
		return
	}

	// Log behavior computation to audit log
	h.logBehaviorAudit(tc, "Computed communication metrics, WLB, stress, relationships, productivity")

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"status":  "complete",
		"message": "All behavior intelligence computed successfully",
	})
}

func (h *BehaviorHandlers) logBehaviorAudit(tc database.TenantContext, detail string) {
	conn, err := h.pool.Acquire(tc)
	if err != nil {
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		return
	}
	_, _ = conn.Exec(tc,
		`INSERT INTO ai_audit_log (user_id, tenant_id, task_type, model_used, tokens_in, tokens_out, latency_ms, created_at)
		 VALUES ($1, $2, 'behavior_analysis', 'system', 0, 0, 0, now())`,
		tc.UserID, tc.TenantID,
	)
}
