package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestWithTenant_CreatesContext(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()

	tc := WithTenant(context.Background(), tenantID, userID)

	assert.Equal(t, tenantID, tc.TenantID)
	assert.Equal(t, userID, tc.UserID)
	assert.NotNil(t, tc.Context)
}

func TestTenantContext_EmbedsSameParentContext(t *testing.T) {
	type key struct{}
	parentCtx := context.WithValue(context.Background(), key{}, "test-value")
	tenantID := uuid.New()
	userID := uuid.New()

	tc := WithTenant(parentCtx, tenantID, userID)

	assert.Equal(t, "test-value", tc.Value(key{}))
}
