package auth

import (
	"context"

	"github.com/google/uuid"
)

// AuthenticatedUser represents a validated user from JWT claims.
type AuthenticatedUser struct {
	ID       uuid.UUID // sub claim
	TenantID uuid.UUID // app_metadata.tenant_id
	Email    string    // email claim
	Role     string    // role claim
	Provider string    // app_metadata.provider
}

type contextKey string

const userKey contextKey = "authenticated_user"

// WithUser stores an AuthenticatedUser in the context.
func WithUser(ctx context.Context, user *AuthenticatedUser) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// GetUser retrieves the AuthenticatedUser from context. Panics if not present.
func GetUser(ctx context.Context) *AuthenticatedUser {
	user, ok := ctx.Value(userKey).(*AuthenticatedUser)
	if !ok || user == nil {
		panic("GetUser called outside auth middleware")
	}
	return user
}

// MaybeGetUser retrieves the AuthenticatedUser if present, without panicking.
func MaybeGetUser(ctx context.Context) (*AuthenticatedUser, bool) {
	user, ok := ctx.Value(userKey).(*AuthenticatedUser)
	return user, ok && user != nil
}
