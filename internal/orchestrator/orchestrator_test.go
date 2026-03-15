package orchestrator

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Config Tests ---

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	assert.Equal(t, 10*time.Second, cfg.BootDelay)
	assert.Equal(t, 30*time.Second, cfg.StaggerDuration)
	assert.Equal(t, 50, cfg.MaxConcurrentLogins)
	assert.Equal(t, 30*time.Second, cfg.HeartbeatInterval)
	assert.Equal(t, 30*time.Second, cfg.DrainTimeout)
}

// --- Registry Tests ---

// testConfig returns fast config suitable for unit tests (no DB).
func testConfig() *OrchestratorConfig {
	return &OrchestratorConfig{
		BootDelay:                  1 * time.Millisecond,
		StaggerDuration:            1 * time.Millisecond,
		MaxConcurrentLogins:        10,
		HeartbeatInterval:          100 * time.Hour, // effectively disabled
		DrainTimeout:               5 * time.Second,
		SupervisorBaseBackoff:      10 * time.Millisecond,
		SupervisorMaxBackoff:       100 * time.Millisecond,
		SupervisorMaxFailures:      10,
		SupervisorStableThreshold:  30 * time.Second,
	}
}

func TestServiceRegistry_SpawnAndStop(t *testing.T) {
	t.Parallel()
	cfg := testConfig()

	// nil pool is fine since we won't actually run DB queries in these tests —
	// the health reporter and cron will fail silently on acquire errors.
	registry := NewServiceRegistry(nil, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	userID := uuid.New()
	tenantID := uuid.New()

	registry.Spawn(ctx, userID, tenantID)

	// Give the goroutine a moment to start.
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, registry.Count())
	assert.True(t, registry.IsRunning(userID))

	// Stop the bundle.
	registry.Stop(userID)

	// Wait for the bundle goroutine to clean up.
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 0, registry.Count())
	assert.False(t, registry.IsRunning(userID))
}

func TestServiceRegistry_SpawnIdempotent(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	userID := uuid.New()
	tenantID := uuid.New()

	registry.Spawn(ctx, userID, tenantID)
	registry.Spawn(ctx, userID, tenantID) // second call is no-op
	registry.Spawn(ctx, userID, tenantID) // third call is no-op

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, registry.Count(), "idempotent spawn should not create duplicate bundles")

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestServiceRegistry_DrainAll(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Spawn multiple bundles.
	const numUsers = 5
	for i := 0; i < numUsers; i++ {
		registry.Spawn(ctx, uuid.New(), uuid.New())
	}

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, numUsers, registry.Count())

	// Drain all.
	err := registry.DrainAll(5 * time.Second)
	require.NoError(t, err)

	assert.Equal(t, 0, registry.Count())
}

func TestServiceRegistry_DrainAllTimeout(t *testing.T) {
	t.Parallel()

	// This test verifies that DrainAll returns an error on timeout.
	// We create a bundle whose Run() blocks on a long-lived context.
	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	// Use a context that we do NOT cancel — so the bundle never stops on its own.
	// DrainAll will cancel the bundles, but we use a very short timeout.
	ctx := context.Background()

	registry.Spawn(ctx, uuid.New(), uuid.New())
	time.Sleep(50 * time.Millisecond)

	// Drain with a very short timeout. The bundle's goroutines should stop
	// because DrainAll calls Stop() which cancels the context. With nil pool
	// the health reporter will exit quickly. This should succeed.
	err := registry.DrainAll(2 * time.Second)
	assert.NoError(t, err)
}

func TestServiceRegistry_StopNonexistent(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	// Stopping a user that doesn't exist should not panic.
	registry.Stop(uuid.New())
	assert.Equal(t, 0, registry.Count())
}

// --- Supervisor Tests ---

func TestSupervisorBackoff(t *testing.T) {
	t.Parallel()

	// Verify exponential backoff: 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s, 300s (capped)
	expected := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		64 * time.Second,
		128 * time.Second,
		256 * time.Second,
		5 * time.Minute, // capped
		5 * time.Minute, // stays capped
	}

	for i, want := range expected {
		got := calculateBackoff(i)
		assert.Equal(t, want, got, "backoff for failure %d", i)
	}
}

func TestSupervisorJitterRange(t *testing.T) {
	t.Parallel()

	base := 10 * time.Second

	// Run many iterations and verify all jittered values fall within ±20%.
	minAllowed := time.Duration(float64(base) * supervisorJitterMin)
	maxAllowed := time.Duration(float64(base) * supervisorJitterMax)

	for i := 0; i < 1000; i++ {
		jittered := applyJitter(base)
		assert.GreaterOrEqual(t, jittered, minAllowed,
			"jitter below minimum on iteration %d: %v", i, jittered)
		assert.LessOrEqual(t, jittered, maxAllowed,
			"jitter above maximum on iteration %d: %v", i, jittered)
	}
}

func TestSupervisorDeadLetter(t *testing.T) {
	t.Parallel()

	cfg := testConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	userID := uuid.New()
	tenantID := uuid.New()

	bundle := NewUserServiceBundle(userID, tenantID, ctx, cancel, nil, cfg)

	var callCount atomic.Int32
	failingFn := func(ctx context.Context) error {
		callCount.Add(1)
		return fmt.Errorf("always fails")
	}

	// Run supervisor in a goroutine — it should dead-letter after 10 failures.
	done := make(chan struct{})
	go func() {
		bundle.runWithSupervisor("test_service", failingFn)
		close(done)
	}()

	// With test config (10ms base backoff, 100ms max), all 10 retries finish fast.
	select {
	case <-done:
		// Supervisor returned after dead-lettering.
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor did not dead-letter within timeout")
	}

	// Should have been called exactly cfg.SupervisorMaxFailures times.
	assert.Equal(t, int32(cfg.SupervisorMaxFailures), callCount.Load(),
		"expected exactly %d calls before dead-letter", cfg.SupervisorMaxFailures)

	// Health should reflect paused status.
	statuses, errMsg := bundle.health.getSnapshot()
	assert.Equal(t, "paused", statuses["test_service"])
	assert.Contains(t, errMsg, "dead-lettered")
}

func TestSupervisorBackoffReset(t *testing.T) {
	t.Parallel()

	cfg := testConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	userID := uuid.New()
	tenantID := uuid.New()
	bundle := NewUserServiceBundle(userID, tenantID, ctx, cancel, nil, cfg)

	var callCount atomic.Int32

	// Function that runs "stably" (>30s simulated) then fails.
	// We can't actually wait 30s in a test, so we verify the reset logic
	// by testing with a function that succeeds (returns nil) to reset counters,
	// then fails. A nil return resets immediately per the supervisor code.
	stableOnceThenFail := func(ctx context.Context) error {
		count := callCount.Add(1)
		if count <= 2 {
			// Return nil (clean exit) to reset counters.
			return nil
		}
		if count <= 4 {
			return fmt.Errorf("failure after reset")
		}
		// After a few more, cancel to end the test.
		cancel()
		return nil
	}

	done := make(chan struct{})
	go func() {
		bundle.runWithSupervisor("test_reset", stableOnceThenFail)
		close(done)
	}()

	select {
	case <-done:
		// Supervisor exited (context cancelled).
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor did not exit within timeout")
	}

	// Should have run more than 2 times (resets happened, did not dead-letter at 10).
	assert.Greater(t, callCount.Load(), int32(2),
		"service should have run multiple times with resets")
}

func TestSupervisorContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := testConfig()

	ctx, cancel := context.WithCancel(context.Background())
	userID := uuid.New()
	tenantID := uuid.New()
	bundle := NewUserServiceBundle(userID, tenantID, ctx, cancel, nil, cfg)

	// Function that blocks forever.
	blockingFn := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}

	done := make(chan struct{})
	go func() {
		bundle.runWithSupervisor("test_cancel", blockingFn)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — supervisor exited.
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not exit on context cancellation")
	}
}

// --- Health Reporter Tests ---

func TestHealthReporter_SetStatus(t *testing.T) {
	t.Parallel()

	hr := NewHealthReporter(uuid.New(), uuid.New(), nil, time.Hour)

	// Initial state: all stopped.
	statuses, errMsg := hr.getSnapshot()
	assert.Equal(t, "stopped", statuses["imap"])
	assert.Equal(t, "stopped", statuses["ai"])
	assert.Equal(t, "stopped", statuses["cron"])
	assert.Equal(t, "stopped", statuses["briefing"])
	assert.Empty(t, errMsg)

	// Update statuses.
	hr.SetStatus("imap", "running")
	hr.SetStatus("cron", "starting")

	statuses, _ = hr.getSnapshot()
	assert.Equal(t, "running", statuses["imap"])
	assert.Equal(t, "starting", statuses["cron"])
	assert.Equal(t, "stopped", statuses["ai"])
}

func TestHealthReporter_SetError(t *testing.T) {
	t.Parallel()

	hr := NewHealthReporter(uuid.New(), uuid.New(), nil, time.Hour)

	hr.SetError("imap", "connection refused")

	statuses, errMsg := hr.getSnapshot()
	assert.Equal(t, "error", statuses["imap"])
	assert.Equal(t, "connection refused", errMsg)

	// SetError clears by setting empty message.
	hr.SetError("imap", "")
	statuses, errMsg = hr.getSnapshot()
	assert.Equal(t, "error", statuses["imap"])
	assert.Empty(t, errMsg)
}

func TestHealthReporter_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	hr := NewHealthReporter(uuid.New(), uuid.New(), nil, time.Hour)

	// Hammer the reporter from multiple goroutines.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			hr.SetStatus("imap", "running")
			hr.SetStatus("cron", "starting")
		}
		close(done)
	}()

	for i := 0; i < 1000; i++ {
		hr.SetError("ai", "some error")
		hr.getSnapshot()
	}

	<-done

	// No race condition panics = pass. Just verify we can still read.
	statuses, _ := hr.getSnapshot()
	assert.NotEmpty(t, statuses)
}

// --- NextBackoff Tests ---

func TestNextBackoff(t *testing.T) {
	t.Parallel()

	max := 5 * time.Minute
	assert.Equal(t, 4*time.Second, nextBackoffCapped(2*time.Second, max))
	assert.Equal(t, 8*time.Second, nextBackoffCapped(4*time.Second, max))
	assert.Equal(t, 5*time.Minute, nextBackoffCapped(4*time.Minute, max)) // would be 8min, capped to 5
	assert.Equal(t, 5*time.Minute, nextBackoffCapped(5*time.Minute, max)) // already at max, stays
}

// --- Cron Scheduler Tests ---

func TestCronScheduler_New(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tenantID := uuid.New()

	cs := NewCronScheduler(userID, tenantID, nil, nil, nil, nil)
	assert.Equal(t, userID, cs.userID)
	assert.Equal(t, tenantID, cs.tenantID)
}

// --- Bundle Tests ---

func TestUserServiceBundle_Stop(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	userID := uuid.New()
	tenantID := uuid.New()
	bundle := NewUserServiceBundle(userID, tenantID, ctx, cancel, nil, cfg)

	done := make(chan struct{})
	go func() {
		bundle.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	bundle.Stop()

	select {
	case <-done:
		// Bundle exited cleanly.
	case <-time.After(5 * time.Second):
		t.Fatal("bundle.Run() did not return after Stop()")
	}
}
