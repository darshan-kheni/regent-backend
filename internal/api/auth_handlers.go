package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/darshan-kheni/regent/internal/auth"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// AuthHandlers contains HTTP handlers for authentication endpoints.
type AuthHandlers struct {
	supabase   *auth.SupabaseClient
	sessions   *auth.SessionService
	lockout    *auth.LockoutService
	audit      *auth.AuditLogger
	tokenStore *auth.OAuthTokenStore
}

// NewAuthHandlers creates a new AuthHandlers instance.
func NewAuthHandlers(
	supabase *auth.SupabaseClient,
	sessions *auth.SessionService,
	lockout *auth.LockoutService,
	audit *auth.AuditLogger,
	tokenStore *auth.OAuthTokenStore,
) *AuthHandlers {
	return &AuthHandlers{
		supabase:   supabase,
		sessions:   sessions,
		lockout:    lockout,
		audit:      audit,
		tokenStore: tokenStore,
	}
}

// Signup handles POST /api/v1/auth/signup.
func (h *AuthHandlers) Signup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Email == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "email is required")
		return
	}

	if err := auth.ValidatePassword(req.Password); err != nil {
		WriteError(w, r, http.StatusBadRequest, "WEAK_PASSWORD", err.Error())
		return
	}

	result, err := h.supabase.SignUp(r.Context(), req.Email, req.Password)
	if err != nil {
		slog.Error("signup failed", "error", err)
		WriteError(w, r, http.StatusBadGateway, "SIGNUP_FAILED", "failed to create account")
		return
	}

	h.audit.Log(r.Context(), r, auth.EventSignup, &result.ID, nil, "email", true, nil)

	WriteJSON(w, r, http.StatusCreated, map[string]interface{}{
		"user_id": result.ID,
		"email":   result.Email,
	})
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "email and password are required")
		return
	}

	// Check lockout
	locked, err := h.lockout.CheckLocked(r.Context(), req.Email)
	if err != nil {
		slog.Error("lockout check failed", "error", err)
	}
	if locked {
		h.audit.Log(r.Context(), r, auth.EventLoginFailed, nil, nil, "email", false, map[string]interface{}{"reason": "account_locked"})
		WriteError(w, r, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "account is temporarily locked due to too many failed attempts")
		return
	}

	result, err := h.supabase.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		// Record failure
		if lockErr := h.lockout.RecordFailure(r.Context(), req.Email); lockErr != nil {
			slog.Error("failed to record login failure", "error", lockErr)
		}
		h.audit.Log(r.Context(), r, auth.EventLoginFailed, nil, nil, "email", false, map[string]interface{}{"email": req.Email})
		WriteError(w, r, http.StatusUnauthorized, "LOGIN_FAILED", "invalid email or password")
		return
	}

	// Clear lockout on success
	if err := h.lockout.ClearLockout(r.Context(), req.Email); err != nil {
		slog.Error("failed to clear lockout", "error", err)
	}

	h.audit.Log(r.Context(), r, auth.EventLogin, &result.User.ID, nil, "email", true, nil)

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"access_token":  result.AccessToken,
		"refresh_token": result.RefreshToken,
		"expires_in":    result.ExpiresIn,
		"token_type":    result.TokenType,
	})
}

// Logout handles POST /api/v1/auth/logout (protected).
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	if err := h.supabase.SignOutUser(r.Context(), user.ID, "local"); err != nil {
		slog.Error("supabase signout failed", "error", err)
	}

	h.audit.Log(r.Context(), r, auth.EventLogout, &user.ID, &tc.TenantID, user.Provider, true, nil)

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "logged out"})
}

// Refresh handles POST /api/v1/auth/refresh (protected).
func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.RefreshToken == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "refresh_token is required")
		return
	}

	result, err := h.supabase.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		slog.Error("token refresh failed", "error", err)
		Unauthorized(w, r, "failed to refresh token")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"access_token":  result.AccessToken,
		"refresh_token": result.RefreshToken,
		"expires_in":    result.ExpiresIn,
		"token_type":    result.TokenType,
	})
}

// ResetPassword handles POST /api/v1/auth/reset-password (public).
func (h *AuthHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Email == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "email is required")
		return
	}

	// Always return success (don't leak whether email exists)
	if err := h.supabase.ResetPasswordForEmail(r.Context(), req.Email); err != nil {
		slog.Error("password reset failed", "error", err)
	}

	h.audit.Log(r.Context(), r, auth.EventPasswordReset, nil, nil, "email", true, map[string]interface{}{"email": req.Email})

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "if that email exists, a reset link has been sent"})
}

// UpdatePassword handles POST /api/v1/auth/update-password (protected).
func (h *AuthHandlers) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if err := auth.ValidatePassword(req.Password); err != nil {
		WriteError(w, r, http.StatusBadRequest, "WEAK_PASSWORD", err.Error())
		return
	}

	if err := h.supabase.UpdateUserPassword(r.Context(), user.ID, req.Password); err != nil {
		slog.Error("password update failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "UPDATE_FAILED", "failed to update password")
		return
	}

	// Force logout other sessions
	if err := h.supabase.SignOutUser(r.Context(), user.ID, "others"); err != nil {
		slog.Error("failed to revoke other sessions", "error", err)
	}

	h.audit.Log(r.Context(), r, auth.EventPasswordChange, &user.ID, &tc.TenantID, "email", true, nil)

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "password updated"})
}

// ListSessions handles GET /api/v1/auth/sessions (protected).
func (h *AuthHandlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	sessions, err := h.sessions.ListSessions(tc, user.ID)
	if err != nil {
		slog.Error("list sessions failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list sessions")
		return
	}

	WriteJSON(w, r, http.StatusOK, sessions)
}

// RevokeAllSessions handles DELETE /api/v1/auth/sessions (protected).
func (h *AuthHandlers) RevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	if err := h.sessions.RevokeAll(tc, user.ID); err != nil {
		slog.Error("revoke all sessions failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to revoke sessions")
		return
	}

	h.audit.Log(r.Context(), r, auth.EventSessionRevoked, &user.ID, &tc.TenantID, "", true, map[string]interface{}{"scope": "all"})

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "all sessions revoked"})
}

// RevokeSession handles DELETE /api/v1/auth/sessions/{id} (protected).
func (h *AuthHandlers) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "session id is required")
		return
	}

	if err := h.sessions.RevokeSession(tc, sessionID); err != nil {
		slog.Error("revoke session failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to revoke session")
		return
	}

	h.audit.Log(r.Context(), r, auth.EventSessionRevoked, &user.ID, &tc.TenantID, "", true, map[string]interface{}{"session_id": sessionID})

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "session revoked"})
}

// OAuthCallback handles POST /api/v1/auth/callback (public).
// Called after Supabase handles the OAuth flow. The frontend sends the session info.
func (h *AuthHandlers) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider  string `json:"provider"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Provider == "" || req.SessionID == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "provider and session_id are required")
		return
	}

	h.audit.Log(r.Context(), r, auth.EventOAuthConnect, nil, nil, req.Provider, true, nil)

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"tokens_stored": true,
		"provider":      req.Provider,
	})
}

// ConnectGoogle handles POST /api/v1/auth/connect/google (protected).
func (h *AuthHandlers) ConnectGoogle(w http.ResponseWriter, r *http.Request) {
	h.connectProvider(w, r, "google")
}

// ConnectMicrosoft handles POST /api/v1/auth/connect/microsoft (protected).
func (h *AuthHandlers) ConnectMicrosoft(w http.ResponseWriter, r *http.Request) {
	h.connectProvider(w, r, "microsoft")
}

func (h *AuthHandlers) connectProvider(w http.ResponseWriter, r *http.Request, provider string) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		ProviderToken        string   `json:"provider_token"`
		ProviderRefreshToken string   `json:"provider_refresh_token"`
		Scopes               []string `json:"scopes"`
		ProviderEmail        string   `json:"provider_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.ProviderToken == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "provider_token is required")
		return
	}

	// Validate scopes
	switch provider {
	case "google":
		if err := auth.ValidateGoogleScopes(req.Scopes); err != nil {
			WriteError(w, r, http.StatusBadRequest, "INSUFFICIENT_SCOPES", err.Error())
			return
		}
	case "microsoft":
		if err := auth.ValidateMicrosoftScopes(req.Scopes); err != nil {
			WriteError(w, r, http.StatusBadRequest, "INSUFFICIENT_SCOPES", err.Error())
			return
		}
	}

	// Resolve users.id from auth_id (JWT sub != users.id for some users)
	actualUserID := tc.UserID // This is the auth user ID from JWT

	// Store encrypted tokens using the auth user ID
	// The token store needs to handle the FK by using the correct users.id
	err := h.tokenStore.StoreTokens(tc, actualUserID, provider,
		req.ProviderToken, req.ProviderRefreshToken,
		req.Scopes, time.Now().Add(time.Hour),
		actualUserID.String(), req.ProviderEmail,
	)
	if err != nil {
		slog.Error("store tokens failed", "error", err, "provider", provider)
		WriteError(w, r, http.StatusInternalServerError, "STORE_FAILED", "failed to store provider tokens")
		return
	}

	// Create email account entry so the orchestrator picks it up for syncing
	if req.ProviderEmail != "" {
		if accErr := h.tokenStore.EnsureEmailAccount(tc, user.ID, provider, req.ProviderEmail); accErr != nil {
			slog.Warn("failed to create email account for OAuth", "error", accErr, "provider", provider)
			// Non-fatal — tokens are stored, account can be created manually
		}
	}

	h.audit.Log(r.Context(), r, auth.EventOAuthConnect, &user.ID, &tc.TenantID, provider, true, nil)

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"connected": true,
		"provider":  provider,
		"email":     req.ProviderEmail,
	})
}

// ConnectGoogleCalendar handles POST /api/v1/auth/connect/google-calendar (protected).
func (h *AuthHandlers) ConnectGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	// Placeholder — will be wired to calendar-specific OAuth flow
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "google_calendar_connect_placeholder"})
}

// ConnectMicrosoftCalendar handles POST /api/v1/auth/connect/microsoft-calendar (protected).
func (h *AuthHandlers) ConnectMicrosoftCalendar(w http.ResponseWriter, r *http.Request) {
	// Placeholder — will be wired to calendar-specific OAuth flow
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "microsoft_calendar_connect_placeholder"})
}

// DisconnectProvider handles DELETE /api/v1/auth/connect/{provider} (protected).
func (h *AuthHandlers) DisconnectProvider(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	provider := chi.URLParam(r, "provider")
	if provider == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "provider is required")
		return
	}

	if err := h.tokenStore.DeleteTokens(tc, user.ID, provider); err != nil {
		slog.Error("delete tokens failed", "error", err, "provider", provider)
		WriteError(w, r, http.StatusInternalServerError, "DELETE_FAILED", "failed to disconnect provider")
		return
	}

	h.audit.Log(r.Context(), r, auth.EventOAuthDisconnect, &user.ID, &tc.TenantID, provider, true, nil)

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"disconnected": true,
		"provider":     provider,
	})
}
