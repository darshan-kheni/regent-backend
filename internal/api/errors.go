package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/darshan-kheni/regent/internal/middleware"
)

// ErrorResponse is the standard JSON error response format.
type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

// WriteError writes a standard error response.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	resp := ErrorResponse{
		Error:     message,
		Code:      code,
		RequestID: middleware.GetRequestID(r.Context()),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// Unauthorized writes a 401 error response.
func Unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", message)
}

// Forbidden writes a 403 error response.
func Forbidden(w http.ResponseWriter, r *http.Request, message string) {
	WriteError(w, r, http.StatusForbidden, "FORBIDDEN", message)
}

// TooManyRequests writes a 429 error response.
func TooManyRequests(w http.ResponseWriter, r *http.Request, message string) {
	WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", message)
}

// PlanRequiredResponse is the 402 response for feature gating.
type PlanRequiredResponse struct {
	Error       string `json:"error"`
	Code        string `json:"code"`
	Gate        string `json:"gate"`
	CurrentPlan string `json:"current_plan"`
	UpgradePlan string `json:"upgrade_plan"`
	UpgradeURL  string `json:"upgrade_url"`
	RequestID   string `json:"request_id"`
	Timestamp   string `json:"timestamp"`
}

// PaymentRequired writes a 402 error response with feature gate information.
func PaymentRequired(w http.ResponseWriter, r *http.Request, gate, currentPlan, upgradePlan string) {
	resp := PlanRequiredResponse{
		Error:       "plan_limit_exceeded",
		Code:        "PLAN_REQUIRED",
		Gate:        gate,
		CurrentPlan: currentPlan,
		UpgradePlan: upgradePlan,
		UpgradeURL:  "/billing",
		RequestID:   middleware.GetRequestID(r.Context()),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(resp)
}
