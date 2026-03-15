package realtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectStrategy_Gmail(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("gmail", []string{"IMAP4rev1", "IDLE", "NAMESPACE"})
	assert.Equal(t, StrategyGmailPush, strategy, "Gmail should always use push, even if IDLE is advertised")
}

func TestDetectStrategy_GmailCaseInsensitive(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("Gmail", nil)
	assert.Equal(t, StrategyGmailPush, strategy, "Gmail detection should be case-insensitive")
}

func TestDetectStrategy_IDLE(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("outlook", []string{"IMAP4rev1", "IDLE", "NAMESPACE"})
	assert.Equal(t, StrategyIDLE, strategy, "should use IDLE when capability is present")
}

func TestDetectStrategy_IDLECaseInsensitive(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("imap", []string{"imap4rev1", "idle", "namespace"})
	assert.Equal(t, StrategyIDLE, strategy, "IDLE detection should be case-insensitive")
}

func TestDetectStrategy_Poll(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("custom-imap", []string{"IMAP4rev1", "NAMESPACE"})
	assert.Equal(t, StrategyPoll, strategy, "should fallback to poll when no IDLE capability")
}

func TestDetectStrategy_EmptyCapabilities(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("some-provider", nil)
	assert.Equal(t, StrategyPoll, strategy, "should fallback to poll with nil capabilities")
}

func TestDetectStrategy_EmptySliceCapabilities(t *testing.T) {
	t.Parallel()
	strategy := DetectStrategy("some-provider", []string{})
	assert.Equal(t, StrategyPoll, strategy, "should fallback to poll with empty capabilities slice")
}

func TestIDLERestartInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, 28*time.Minute, idleRestartInterval,
		"IDLE restart interval must be 28 minutes (RFC 2177 < 30min)")
}

func TestPollInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2*time.Minute, pollInterval,
		"poll interval must be 2 minutes")
}

func TestRealtimeStrategyValues(t *testing.T) {
	t.Parallel()
	assert.Equal(t, RealtimeStrategy("idle"), StrategyIDLE)
	assert.Equal(t, RealtimeStrategy("poll"), StrategyPoll)
	assert.Equal(t, RealtimeStrategy("gmail_push"), StrategyGmailPush)
}

func TestDetectStrategy_MixedCaseProvider(t *testing.T) {
	t.Parallel()
	// Only "gmail" (case-insensitive) triggers push. Other providers go through
	// capability detection.
	tests := []struct {
		name         string
		provider     string
		capabilities []string
		want         RealtimeStrategy
	}{
		{"GMAIL uppercase", "GMAIL", nil, StrategyGmailPush},
		{"yahoo with IDLE", "yahoo", []string{"IDLE"}, StrategyIDLE},
		{"yahoo without IDLE", "yahoo", []string{"IMAP4rev1"}, StrategyPoll},
		{"fastmail with IDLE", "fastmail", []string{"IDLE", "CONDSTORE"}, StrategyIDLE},
		{"empty provider with IDLE", "", []string{"IDLE"}, StrategyIDLE},
		{"empty provider no caps", "", nil, StrategyPoll},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DetectStrategy(tc.provider, tc.capabilities)
			assert.Equal(t, tc.want, got)
		})
	}
}
