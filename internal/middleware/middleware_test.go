package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/darshan-kheni/regent/internal/auth"
)

func TestRequestID_InjectsID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		assert.NotEmpty(t, id)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.NotEmpty(t, rr.Header().Get("X-Request-ID"))
}

func TestRequestID_PreservesExisting(t *testing.T) {
	existingID := "test-request-id-123"
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		assert.Equal(t, existingID, id)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", existingID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, existingID, rr.Header().Get("X-Request-ID"))
}

func TestAuth_Stub_MissingHeaders_Returns401(t *testing.T) {
	authMW, err := NewAuth(testAuthConfig())
	require.NoError(t, err)
	handler := authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuth_Stub_ValidHeaders_PassesThrough(t *testing.T) {
	authMW, err := NewAuth(testAuthConfig())
	require.NoError(t, err)
	handler := authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := getAuthClaim(r.Context(), "tenant_id")
		userID := getAuthClaim(r.Context(), "user_id")
		assert.NotEmpty(t, tenantID)
		assert.NotEmpty(t, userID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	req.Header.Set("X-User-ID", uuid.New().String())
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestTenantScope_JWT_Mode_ReadsAuthenticatedUser(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()

	scopeMW := NewTenantScope()
	handler := scopeMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, ok := GetTenantContext(r.Context())
		require.True(t, ok)
		assert.Equal(t, tenantID, tc.TenantID)
		assert.Equal(t, userID, tc.UserID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.AuthenticatedUser{ID: userID, TenantID: tenantID, Email: "test@example.com", Role: "authenticated"}
	ctx := auth.WithUser(req.Context(), user)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestTenantScope_InvalidTenantID_Returns401(t *testing.T) {
	scopeMW := NewTenantScope()
	handler := scopeMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := withAuthClaim(req.Context(), "tenant_id", "invalid-uuid")
	ctx = withAuthClaim(ctx, "user_id", uuid.New().String())
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestTenantScope_ValidIDs_InjectsContext(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()

	scopeMW := NewTenantScope()
	handler := scopeMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, ok := GetTenantContext(r.Context())
		require.True(t, ok)
		assert.Equal(t, tenantID, tc.TenantID)
		assert.Equal(t, userID, tc.UserID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := withAuthClaim(req.Context(), "tenant_id", tenantID.String())
	ctx = withAuthClaim(ctx, "user_id", userID.String())
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRecoverer_CatchesPanic(t *testing.T) {
	recoverMW := NewRecoverer()
	handler := recoverMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "INTERNAL_ERROR")
}

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	_, rateMW := NewRateLimiterForTesting(5)
	handler := rateMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "request %d should succeed", i)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	_, rateMW := NewRateLimiterForTesting(3)
	handler := rateMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust 3 tokens
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "60", rr.Header().Get("Retry-After"))
}

func TestAuthRateLimiter_WithinLimit(t *testing.T) {
	rl := NewAuthRateLimiterForTesting(3, time.Minute)
	handler := rl.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "request %d should pass", i+1)
	}
}

func TestAuthRateLimiter_ExceedsLimit(t *testing.T) {
	rl := NewAuthRateLimiterForTesting(3, time.Minute)
	handler := rl.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// 4th request should be rate limited
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "60", rr.Header().Get("Retry-After"))
}

func TestAuthRateLimiter_DifferentIPs(t *testing.T) {
	rl := NewAuthRateLimiterForTesting(2, time.Minute)
	handler := rl.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP1: 2 requests (at limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// IP2: should still be allowed
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "2.2.2.2:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// IP1: should be blocked
	req = httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "1.1.1.1:1234"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}
