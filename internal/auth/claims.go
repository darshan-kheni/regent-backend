package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// UserFromClaims extracts an AuthenticatedUser from JWT MapClaims.
func UserFromClaims(claims jwt.MapClaims) (*AuthenticatedUser, error) {
	sub, err := claims.GetSubject()
	if err != nil {
		return nil, fmt.Errorf("missing sub claim: %w", err)
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		return nil, fmt.Errorf("invalid sub UUID: %w", err)
	}

	appMeta, _ := claims["app_metadata"].(map[string]interface{})
	tenantStr, _ := appMeta["tenant_id"].(string)
	tenantID, err := uuid.Parse(tenantStr)
	if err != nil {
		return nil, fmt.Errorf("missing or invalid tenant_id in app_metadata: %w", err)
	}

	email, _ := claims["email"].(string)
	role, _ := claims["role"].(string)
	provider, _ := appMeta["provider"].(string)

	return &AuthenticatedUser{
		ID:       userID,
		TenantID: tenantID,
		Email:    email,
		Role:     role,
		Provider: provider,
	}, nil
}
