package connection

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectionManager_RegisterAndGet(t *testing.T) {
	cm := NewConnectionManager(10)

	accountID := uuid.New()
	// We can't create a real imapclient.Client without a connection,
	// but we can test the registry logic with nil (for unit test purposes)
	err := cm.Register(accountID, nil)
	require.NoError(t, err)

	conn, ok := cm.Get(accountID)
	assert.True(t, ok)
	assert.Equal(t, accountID, conn.AccountID)
	assert.Equal(t, StatusConnected, conn.Status)
	assert.Equal(t, 1, cm.Count())
}

func TestConnectionManager_ConnectionLimit(t *testing.T) {
	cm := NewConnectionManager(2)

	err := cm.Register(uuid.New(), nil)
	require.NoError(t, err)
	err = cm.Register(uuid.New(), nil)
	require.NoError(t, err)

	// Third should fail
	err = cm.Register(uuid.New(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection limit reached")
	assert.Equal(t, 2, cm.Count())
}

func TestConnectionManager_Remove(t *testing.T) {
	cm := NewConnectionManager(10)
	accountID := uuid.New()

	cm.Register(accountID, nil)
	assert.Equal(t, 1, cm.Count())

	cm.Remove(accountID)
	assert.Equal(t, 0, cm.Count())

	_, ok := cm.Get(accountID)
	assert.False(t, ok)
}

func TestConnectionManager_RemoveNonExistent(t *testing.T) {
	cm := NewConnectionManager(10)
	// Should not panic
	cm.Remove(uuid.New())
}

func TestConnectionManager_SetError(t *testing.T) {
	cm := NewConnectionManager(10)
	accountID := uuid.New()
	cm.Register(accountID, nil)

	cm.SetError(accountID, nil)
	conn, _ := cm.Get(accountID)
	assert.Equal(t, StatusError, conn.Status)
	assert.Equal(t, 1, conn.ErrorCount)
}

func TestConnectionManager_ActiveAndErrorCounts(t *testing.T) {
	cm := NewConnectionManager(10)
	a1, a2, a3 := uuid.New(), uuid.New(), uuid.New()

	cm.Register(a1, nil)
	cm.Register(a2, nil)
	cm.Register(a3, nil)

	assert.Equal(t, 3, cm.ActiveCount())
	assert.Equal(t, 0, cm.ErrorCount())

	cm.SetError(a2, nil)
	assert.Equal(t, 2, cm.ActiveCount())
	assert.Equal(t, 1, cm.ErrorCount())
}

func TestCalculateBackoff(t *testing.T) {
	base := 2 * time.Second
	max := 5 * time.Minute

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{4, 32 * time.Second},
		{5, 64 * time.Second},
		{6, 128 * time.Second},
		{7, 256 * time.Second},
		{8, 5 * time.Minute}, // Capped at max
		{9, 5 * time.Minute}, // Still capped
	}

	for _, tt := range tests {
		result := CalculateBackoff(tt.attempt, base, max)
		assert.Equal(t, tt.expected, result, "attempt %d", tt.attempt)
	}
}

func TestApplyJitter(t *testing.T) {
	base := 10 * time.Second
	minExpected := time.Duration(float64(base) * 0.8)
	maxExpected := time.Duration(float64(base) * 1.2)

	// Run multiple times to verify range
	for i := 0; i < 100; i++ {
		jittered := applyJitter(base)
		assert.GreaterOrEqual(t, jittered, minExpected, "jitter too low")
		assert.LessOrEqual(t, jittered, maxExpected, "jitter too high")
	}
}

func TestConnectionManager_CloseAll(t *testing.T) {
	cm := NewConnectionManager(10)
	cm.Register(uuid.New(), nil)
	cm.Register(uuid.New(), nil)
	cm.Register(uuid.New(), nil)
	assert.Equal(t, 3, cm.Count())

	cm.CloseAll()
	assert.Equal(t, 0, cm.Count())
}
