package billing

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/database"
)

// TenantExtractor is a function that extracts TenantContext from an HTTP request.
// This avoids an import cycle between billing and middleware packages.
// Callers should pass middleware.GetTenantContext wrapped in a request-aware function.
type TenantExtractor func(r *http.Request) (database.TenantContext, bool)

// GateResponse is the 402 error response for plan-gated features.
type GateResponse struct {
	Error       string `json:"error"`
	Code        string `json:"code"`
	Gate        string `json:"gate"`
	CurrentPlan string `json:"current_plan"`
	UpgradePlan string `json:"upgrade_plan"`
	UpgradeURL  string `json:"upgrade_url"`
	RequestID   string `json:"request_id"`
	Timestamp   string `json:"timestamp"`
}

// writeGateError writes a 402 Payment Required response with gate info.
func writeGateError(w http.ResponseWriter, requestID, gate, currentPlan, upgradePlan string) {
	resp := GateResponse{
		Error:       "plan_limit_exceeded",
		Code:        "PLAN_REQUIRED",
		Gate:        gate,
		CurrentPlan: currentPlan,
		UpgradePlan: upgradePlan,
		UpgradeURL:  "/billing",
		RequestID:   requestID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(resp)
}

// getRequestID extracts the request ID from the response header (set by RequestID middleware).
func getRequestID(w http.ResponseWriter) string {
	return w.Header().Get("X-Request-ID")
}

// RequireFeature returns middleware that blocks requests if the tenant's plan
// does not include the named feature. Returns 402 with upgrade info.
// The extract function should wrap middleware.GetTenantContext.
func RequireFeature(name string, extract TenantExtractor, rdb *redis.Client, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := extract(r)
			if !ok {
				http.Error(w, `{"error":"unauthorized","code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
				return
			}

			plan, err := GetCachedPlan(r.Context(), tc.TenantID, rdb, pool)
			if err != nil {
				slog.Error("failed to get plan for feature gate",
					"tenant_id", tc.TenantID,
					"feature", name,
					"error", err,
				)
				// Fail open: allow the request if we can't determine the plan.
				next.ServeHTTP(w, r)
				return
			}

			if !HasFeature(plan, name) {
				upgradePlan := SuggestUpgrade(plan, name)
				writeGateError(w, getRequestID(w), name, plan, upgradePlan)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAccounts returns middleware that blocks requests if the tenant has
// reached their maximum account limit. The getCount function should return
// the tenant's current number of connected accounts.
func RequireAccounts(getCount func(database.TenantContext) (int, error), extract TenantExtractor, rdb *redis.Client, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := extract(r)
			if !ok {
				http.Error(w, `{"error":"unauthorized","code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
				return
			}

			plan, err := GetCachedPlan(r.Context(), tc.TenantID, rdb, pool)
			if err != nil {
				slog.Error("failed to get plan for account gate",
					"tenant_id", tc.TenantID,
					"error", err,
				)
				next.ServeHTTP(w, r)
				return
			}

			planDef, ok := GetPlanByName(plan)
			if !ok {
				planDef, _ = GetPlanByName(PlanFree)
			}

			// 0 means unlimited.
			if planDef.Limits.MaxAccounts == 0 {
				next.ServeHTTP(w, r)
				return
			}

			count, err := getCount(tc)
			if err != nil {
				slog.Error("failed to count accounts for gate",
					"tenant_id", tc.TenantID,
					"error", err,
				)
				// Fail open on count errors.
				next.ServeHTTP(w, r)
				return
			}

			if count >= planDef.Limits.MaxAccounts {
				upgradePlan := nextPlan(plan)
				writeGateError(w, getRequestID(w), "max_accounts", plan, upgradePlan)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireTokens returns middleware that blocks requests if the tenant has
// exceeded their daily token limit. The getAmount function should return the
// estimated token cost for the request.
func RequireTokens(getAmount func(*http.Request) int64, extract TenantExtractor, rdb *redis.Client, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := extract(r)
			if !ok {
				http.Error(w, `{"error":"unauthorized","code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
				return
			}

			plan, err := GetCachedPlan(r.Context(), tc.TenantID, rdb, pool)
			if err != nil {
				slog.Error("failed to get plan for token gate",
					"tenant_id", tc.TenantID,
					"error", err,
				)
				next.ServeHTTP(w, r)
				return
			}

			planDef, ok := GetPlanByName(plan)
			if !ok {
				planDef, _ = GetPlanByName(PlanFree)
			}

			// 0 means unlimited.
			if planDef.Limits.DailyTokens == 0 {
				next.ServeHTTP(w, r)
				return
			}

			allowed, current, limit, err := CheckDailyTokenLimit(tc, pool)
			if err != nil {
				slog.Error("failed to check daily token limit",
					"tenant_id", tc.TenantID,
					"error", err,
				)
				next.ServeHTTP(w, r)
				return
			}

			requestedTokens := getAmount(r)
			if !allowed || (current+requestedTokens) > limit {
				upgradePlan := nextPlan(plan)
				writeGateError(w, getRequestID(w), "daily_tokens", plan, upgradePlan)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// nextPlan returns the next higher plan name, or empty string for the highest plan.
func nextPlan(current string) string {
	ordered := []string{PlanFree, PlanAttache, PlanPrivyCouncil, PlanEstate}
	for i, p := range ordered {
		if p == current && i+1 < len(ordered) {
			return ordered[i+1]
		}
	}
	return ""
}
