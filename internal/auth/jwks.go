package auth

import (
	"fmt"

	"github.com/MicahParks/keyfunc/v3"
)

// NewJWKS creates a JWKS keyfunc that auto-refreshes from the given URL.
func NewJWKS(jwksURL string) (keyfunc.Keyfunc, error) {
	k, err := keyfunc.NewDefault([]string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("create JWKS keyfunc: %w", err)
	}
	return k, nil
}
