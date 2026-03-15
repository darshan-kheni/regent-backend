package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/auth"
	"github.com/darshan-kheni/regent/internal/database"
)

type authClaimKey string

func withAuthClaim(ctx context.Context, key, value string) context.Context {
	return context.WithValue(ctx, authClaimKey(key), value)
}

func getAuthClaim(ctx context.Context, key string) string {
	if v, ok := ctx.Value(authClaimKey(key)).(string); ok {
		return v
	}
	return ""
}

type tenantContextKey struct{}

// NewTenantScope extracts tenant_id and user_id from auth claims,
// creates a TenantContext, and injects it into the request context.
func NewTenantScope() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try AuthenticatedUser first (JWT mode)
			if user, ok := auth.MaybeGetUser(r.Context()); ok {
				tc := database.WithTenant(r.Context(), user.TenantID, user.ID)
				ctx := context.WithValue(tc, tenantContextKey{}, tc)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Fallback to raw claims (stub mode)
			tenantIDStr := getAuthClaim(r.Context(), "tenant_id")
			userIDStr := getAuthClaim(r.Context(), "user_id")

			tenantID, err := uuid.Parse(tenantIDStr)
			if err != nil {
				writeJSONError(w, r, http.StatusUnauthorized, "INVALID_TENANT", "invalid tenant_id")
				return
			}

			userID, err := uuid.Parse(userIDStr)
			if err != nil {
				writeJSONError(w, r, http.StatusUnauthorized, "INVALID_USER", "invalid user_id")
				return
			}

			tc := database.WithTenant(r.Context(), tenantID, userID)
			ctx := context.WithValue(tc, tenantContextKey{}, tc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetTenantContext retrieves the TenantContext from the request context.
func GetTenantContext(ctx context.Context) (database.TenantContext, bool) {
	tc, ok := ctx.Value(tenantContextKey{}).(database.TenantContext)
	return tc, ok
}
