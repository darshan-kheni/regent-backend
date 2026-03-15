package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/billing"
)

// planGateResponse is the 402 error response for plan-gated features.
type planGateResponse struct {
	Error       string `json:"error"`
	Code        string `json:"code"`
	Gate        string `json:"gate"`
	CurrentPlan string `json:"current_plan"`
	UpgradePlan string `json:"upgrade_plan"`
	UpgradeURL  string `json:"upgrade_url"`
	RequestID   string `json:"request_id"`
	Timestamp   string `json:"timestamp"`
}

// writePlanGateError writes a 402 Payment Required response with upgrade info.
func writePlanGateError(w http.ResponseWriter, r *http.Request, gate, currentPlan, upgradePlan string) {
	resp := planGateResponse{
		Error:       "plan_limit_exceeded",
		Code:        "PLAN_REQUIRED",
		Gate:        gate,
		CurrentPlan: currentPlan,
		UpgradePlan: upgradePlan,
		UpgradeURL:  "/billing",
		RequestID:   GetRequestID(r.Context()),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(resp)
}

// PlanGate returns middleware that blocks requests if the tenant's plan does
// not include the named feature. Uses billing.GetCachedPlan for plan lookup
// and billing.HasFeature for the check.
func PlanGate(feature string, rdb *redis.Client, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := GetTenantContext(r.Context())
			if !ok {
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
				return
			}

			plan, err := billing.GetCachedPlan(r.Context(), tc.TenantID, rdb, pool)
			if err != nil {
				slog.Error("plan gate: failed to get plan",
					"tenant_id", tc.TenantID,
					"feature", feature,
					"error", err,
				)
				// Fail open: allow the request if we can't determine the plan.
				next.ServeHTTP(w, r)
				return
			}

			if !billing.HasFeature(plan, feature) {
				upgradePlan := billing.SuggestUpgrade(plan, feature)
				slog.Info("plan gate: feature blocked",
					"tenant_id", tc.TenantID,
					"feature", feature,
					"current_plan", plan,
					"upgrade_plan", upgradePlan,
				)
				writePlanGateError(w, r, feature, plan, upgradePlan)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePlan returns middleware that blocks requests if the tenant's plan
// does not meet the minimum plan tier. Uses billing.PlanAtLeast for comparison.
func RequirePlan(minPlan string, rdb *redis.Client, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := GetTenantContext(r.Context())
			if !ok {
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
				return
			}

			plan, err := billing.GetCachedPlan(r.Context(), tc.TenantID, rdb, pool)
			if err != nil {
				slog.Error("require plan: failed to get plan",
					"tenant_id", tc.TenantID,
					"min_plan", minPlan,
					"error", err,
				)
				// Fail open.
				next.ServeHTTP(w, r)
				return
			}

			if !billing.PlanAtLeast(plan, minPlan) {
				slog.Info("require plan: plan too low",
					"tenant_id", tc.TenantID,
					"current_plan", plan,
					"min_plan", minPlan,
				)
				writePlanGateError(w, r, "min_plan:"+minPlan, plan, minPlan)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
