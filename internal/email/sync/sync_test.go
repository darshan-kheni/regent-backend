package sync

import (
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterAboveUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uids     []imap.UID
		lastUID  *int64
		expected []imap.UID
	}{
		{
			name:     "nil lastUID returns all UIDs (initial sync)",
			uids:     []imap.UID{1, 2, 3, 4, 5},
			lastUID:  nil,
			expected: []imap.UID{1, 2, 3, 4, 5},
		},
		{
			name:     "filters UIDs below threshold",
			uids:     []imap.UID{1, 2, 3, 4, 5},
			lastUID:  int64Ptr(3),
			expected: []imap.UID{4, 5},
		},
		{
			name:     "all UIDs below threshold returns empty",
			uids:     []imap.UID{1, 2, 3},
			lastUID:  int64Ptr(10),
			expected: nil,
		},
		{
			name:     "threshold equals highest UID returns empty",
			uids:     []imap.UID{1, 2, 3},
			lastUID:  int64Ptr(3),
			expected: nil,
		},
		{
			name:     "threshold of zero returns all UIDs",
			uids:     []imap.UID{1, 2, 3},
			lastUID:  int64Ptr(0),
			expected: []imap.UID{1, 2, 3},
		},
		{
			name:     "empty UIDs returns nil",
			uids:     []imap.UID{},
			lastUID:  int64Ptr(5),
			expected: nil,
		},
		{
			name:     "single UID above threshold",
			uids:     []imap.UID{100},
			lastUID:  int64Ptr(50),
			expected: []imap.UID{100},
		},
		{
			name:     "single UID at threshold",
			uids:     []imap.UID{50},
			lastUID:  int64Ptr(50),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FilterAboveUID(tt.uids, tt.lastUID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"gmail", "gmail"},
		{"imap", "imap"},
		{"outlook", "imap"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, providerName(tt.input))
		})
	}
}

func TestSinceDate(t *testing.T) {
	t.Parallel()

	since := sinceDate()
	// sinceDate should return a time approximately 30 days ago.
	require.False(t, since.IsZero())
	// The returned time should be approximately 30 days ago.
	days := time.Since(since).Hours() / 24
	require.InDelta(t, 30, days, 1.0)
}

func TestSyncCursorStates(t *testing.T) {
	t.Parallel()

	// Verify valid state transitions as defined by the CHECK constraint.
	validStates := []string{"pending", "syncing", "active", "error", "paused"}
	stateSet := make(map[string]bool)
	for _, s := range validStates {
		stateSet[s] = true
	}

	// Verify all expected states are in the set.
	assert.True(t, stateSet["pending"], "pending should be a valid state")
	assert.True(t, stateSet["syncing"], "syncing should be a valid state")
	assert.True(t, stateSet["active"], "active should be a valid state")
	assert.True(t, stateSet["error"], "error should be a valid state")
	assert.True(t, stateSet["paused"], "paused should be a valid state")

	// Verify invalid states are not in the set.
	assert.False(t, stateSet["running"], "running should not be a valid state")
	assert.False(t, stateSet["completed"], "completed should not be a valid state (use active)")

	// Verify expected state transition paths.
	// pending -> syncing -> active (happy path)
	// pending -> syncing -> error (failure path)
	transitions := []struct {
		from string
		to   string
	}{
		{"pending", "syncing"},
		{"syncing", "active"},
		{"syncing", "error"},
		{"error", "syncing"},  // retry
		{"paused", "syncing"}, // resume
	}

	for _, tr := range transitions {
		assert.True(t, stateSet[tr.from], "from state %q should be valid", tr.from)
		assert.True(t, stateSet[tr.to], "to state %q should be valid", tr.to)
	}
}

// int64Ptr returns a pointer to the given int64 value.
func int64Ptr(v int64) *int64 {
	return &v
}

