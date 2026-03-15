package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	cfg := NewConfig("https://test.supabase.co", "anon-key", "service-key")
	assert.Equal(t, "https://test.supabase.co/auth/v1/.well-known/jwks.json", cfg.JWKSURL)
	assert.Equal(t, "https://test.supabase.co/auth/v1", cfg.Issuer)
}

func TestWithUser_GetUser(t *testing.T) {
	user := &AuthenticatedUser{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Email:    "test@example.com",
		Role:     "authenticated",
	}

	ctx := WithUser(context.Background(), user)
	got := GetUser(ctx)

	assert.Equal(t, user.ID, got.ID)
	assert.Equal(t, user.TenantID, got.TenantID)
	assert.Equal(t, user.Email, got.Email)
}

func TestGetUser_Panics_WhenNotSet(t *testing.T) {
	assert.Panics(t, func() {
		GetUser(context.Background())
	})
}

func TestMaybeGetUser_ReturnsNil_WhenNotSet(t *testing.T) {
	user, ok := MaybeGetUser(context.Background())
	assert.False(t, ok)
	assert.Nil(t, user)
}

func TestMaybeGetUser_ReturnsUser_WhenSet(t *testing.T) {
	expected := &AuthenticatedUser{ID: uuid.New(), TenantID: uuid.New()}
	ctx := WithUser(context.Background(), expected)

	user, ok := MaybeGetUser(ctx)
	assert.True(t, ok)
	assert.Equal(t, expected.ID, user.ID)
}

func TestUserFromClaims_Valid(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	claims := jwt.MapClaims{
		"sub":   userID.String(),
		"email": "user@example.com",
		"role":  "authenticated",
		"app_metadata": map[string]interface{}{
			"tenant_id": tenantID.String(),
			"provider":  "google",
		},
	}

	user, err := UserFromClaims(claims)
	require.NoError(t, err)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, tenantID, user.TenantID)
	assert.Equal(t, "user@example.com", user.Email)
	assert.Equal(t, "authenticated", user.Role)
	assert.Equal(t, "google", user.Provider)
}

func TestUserFromClaims_MissingSub(t *testing.T) {
	claims := jwt.MapClaims{
		"email": "user@example.com",
		"app_metadata": map[string]interface{}{
			"tenant_id": uuid.New().String(),
		},
	}

	_, err := UserFromClaims(claims)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sub")
}

func TestUserFromClaims_InvalidSubUUID(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": "not-a-uuid",
		"app_metadata": map[string]interface{}{
			"tenant_id": uuid.New().String(),
		},
	}

	_, err := UserFromClaims(claims)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sub UUID")
}

func TestUserFromClaims_MissingTenantID(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":          uuid.New().String(),
		"app_metadata": map[string]interface{}{},
	}

	_, err := UserFromClaims(claims)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id")
}

func TestUserFromClaims_NoAppMetadata(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": uuid.New().String(),
	}

	_, err := UserFromClaims(claims)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id")
}

func TestUserFromClaims_OptionalFieldsMissing(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	claims := jwt.MapClaims{
		"sub": userID.String(),
		"app_metadata": map[string]interface{}{
			"tenant_id": tenantID.String(),
		},
	}

	user, err := UserFromClaims(claims)
	require.NoError(t, err)
	assert.Equal(t, "", user.Email)
	assert.Equal(t, "", user.Role)
	assert.Equal(t, "", user.Provider)
}

func TestSupabaseClient_Created(t *testing.T) {
	cfg := NewConfig("https://test.supabase.co", "anon", "service")
	client := NewSupabaseClient(cfg)
	assert.NotNil(t, client)
}
