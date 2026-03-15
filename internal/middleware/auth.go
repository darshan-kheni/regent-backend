package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/darshan-kheni/regent/internal/auth"
	"github.com/darshan-kheni/regent/internal/config"
)

// NewAuth returns auth middleware. In "stub" mode, reads X-Tenant-ID/X-User-ID headers.
// In "jwt" mode, validates Supabase JWTs via JWKS.
// Returns an error if JWT setup fails (e.g., unreachable JWKS endpoint).
func NewAuth(cfg config.AuthConfig) (func(http.Handler) http.Handler, error) {
	if cfg.Mode == "stub" {
		return newAuthStub(), nil
	}
	return newJWTAuth(cfg)
}

func newAuthStub() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := r.Header.Get("X-Tenant-ID")
			userID := r.Header.Get("X-User-ID")

			if tenantID == "" || userID == "" {
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "missing authentication")
				return
			}

			ctx := r.Context()
			ctx = withAuthClaim(ctx, "tenant_id", tenantID)
			ctx = withAuthClaim(ctx, "user_id", userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newJWTAuth(cfg config.AuthConfig) (func(http.Handler) http.Handler, error) {
	authCfg := auth.NewConfig(cfg.SupabaseURL, cfg.SupabaseAnonKey, cfg.SupabaseServiceKey)
	jwks, err := auth.NewJWKS(authCfg.JWKSURL)
	if err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "missing authorization header")
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			token, err := jwt.Parse(tokenStr, jwks.KeyfuncCtx(r.Context()),
				jwt.WithIssuer(authCfg.Issuer),
				jwt.WithValidMethods([]string{"RS256", "ES256"}),
				jwt.WithExpirationRequired(),
			)
			if err != nil || !token.Valid {
				slog.Debug("JWT validation failed", "error", err)
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "invalid or expired token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "invalid token claims")
				return
			}

			user, err := auth.UserFromClaims(claims)
			if err != nil {
				slog.Debug("claims extraction failed", "error", err)
				writeJSONError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "invalid user claims")
				return
			}

			ctx := auth.WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}, nil
}
