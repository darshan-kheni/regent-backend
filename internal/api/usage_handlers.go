package api

import (
	"log/slog"
	"net/http"

	"github.com/darshan-kheni/regent/internal/billing"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// UsageHandlers provides HTTP handlers for usage metering endpoints.
type UsageHandlers struct {
	usage *billing.UsageService
}

// NewUsageHandlers creates a new UsageHandlers.
func NewUsageHandlers(usage *billing.UsageService) *UsageHandlers {
	return &UsageHandlers{usage: usage}
}

// HandleGetUsage returns usage data and breakdown for the authenticated tenant.
// Query params:
//   - period: "daily", "weekly", or "monthly" (default "daily")
func (h *UsageHandlers) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "daily"
	}

	usage, err := h.usage.GetUsage(tc, period)
	if err != nil {
		slog.Error("failed to get usage data",
			"tenant_id", tc.TenantID,
			"period", period,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retrieve usage data")
		return
	}

	breakdown, err := h.usage.GetUsageBreakdown(tc, period)
	if err != nil {
		slog.Error("failed to get usage breakdown",
			"tenant_id", tc.TenantID,
			"period", period,
			"error", err,
		)
		// Return usage without breakdown rather than failing entirely.
		WriteJSON(w, r, http.StatusOK, map[string]interface{}{
			"usage":     usage,
			"breakdown": []billing.ServiceBreakdown{},
		})
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"usage":     usage,
		"breakdown": breakdown,
	})
}

// HandleGetLimits returns the current plan limits for the authenticated tenant.
func (h *UsageHandlers) HandleGetLimits(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	// Check token limit.
	tokenAllowed, tokenCurrent, tokenLimit, err := billing.CheckDailyTokenLimit(tc, h.usage.Pool())
	if err != nil {
		slog.Error("failed to check token limit",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to check limits")
		return
	}

	// Check email limit.
	emailAllowed, emailCurrent, emailLimit, err := billing.CheckUsageLimit(tc, h.usage.Pool(), "emails_processed")
	if err != nil {
		slog.Error("failed to check email limit",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to check limits")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"tokens": map[string]interface{}{
			"allowed": tokenAllowed,
			"current": tokenCurrent,
			"limit":   tokenLimit,
		},
		"emails": map[string]interface{}{
			"allowed": emailAllowed,
			"current": emailCurrent,
			"limit":   emailLimit,
		},
	})
}
