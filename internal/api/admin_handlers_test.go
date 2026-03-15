package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdminHandlers(t *testing.T) {
	t.Parallel()

	// Passing nil pool is valid for construction — the pool is only used at request time.
	ah := NewAdminHandlers(nil)
	require.NotNil(t, ah)
	assert.Nil(t, ah.pool)
}
