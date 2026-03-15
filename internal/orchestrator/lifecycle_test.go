package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegistryBootDrain verifies that multiple bundles can be spawned and then
// drained within the timeout, confirming the full lifecycle with nil pool.
func TestRegistryBootDrain(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.BootDelay = 0
	cfg.DrainTimeout = 5 * time.Second

	registry := NewServiceRegistry(nil, cfg)

	ctx := context.Background()
	registry.Spawn(ctx, uuid.New(), uuid.New())
	registry.Spawn(ctx, uuid.New(), uuid.New())

	// Give goroutines a moment to start.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 2, registry.Count())

	// DrainAll should complete well within timeout.
	start := time.Now()
	err := registry.DrainAll(cfg.DrainTimeout)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), cfg.DrainTimeout)
	assert.Equal(t, 0, registry.Count())
}

// TestRegistrySpawnIdempotent_Lifecycle verifies that duplicate Spawn calls
// for the same user are no-ops and drain works correctly afterward.
func TestRegistrySpawnIdempotent_Lifecycle(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.BootDelay = 0
	registry := NewServiceRegistry(nil, cfg)

	userID := uuid.New()
	tenantID := uuid.New()
	ctx := context.Background()

	registry.Spawn(ctx, userID, tenantID)
	registry.Spawn(ctx, userID, tenantID) // Should be no-op

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, registry.Count())
	assert.True(t, registry.IsRunning(userID))

	err := registry.DrainAll(5 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, registry.Count())
	assert.False(t, registry.IsRunning(userID))
}

// TestRegistryMultipleUsers_IndependentStop verifies that stopping one user
// does not affect other running bundles.
func TestRegistryMultipleUsers_IndependentStop(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	ctx := context.Background()

	user1 := uuid.New()
	user2 := uuid.New()
	user3 := uuid.New()

	registry.Spawn(ctx, user1, uuid.New())
	registry.Spawn(ctx, user2, uuid.New())
	registry.Spawn(ctx, user3, uuid.New())

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 3, registry.Count())

	// Stop user2 only.
	registry.Stop(user2)
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 2, registry.Count())
	assert.True(t, registry.IsRunning(user1))
	assert.False(t, registry.IsRunning(user2))
	assert.True(t, registry.IsRunning(user3))

	// Clean up remaining.
	err := registry.DrainAll(5 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, registry.Count())
}

// TestRegistryRespawnAfterStop verifies that a user can be re-spawned
// after their bundle has been stopped and cleaned up.
func TestRegistryRespawnAfterStop(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	ctx := context.Background()
	userID := uuid.New()
	tenantID := uuid.New()

	// Spawn, stop, wait for cleanup.
	registry.Spawn(ctx, userID, tenantID)
	time.Sleep(50 * time.Millisecond)
	assert.True(t, registry.IsRunning(userID))

	registry.Stop(userID)
	time.Sleep(100 * time.Millisecond)
	assert.False(t, registry.IsRunning(userID))

	// Re-spawn the same user.
	registry.Spawn(ctx, userID, tenantID)
	time.Sleep(50 * time.Millisecond)
	assert.True(t, registry.IsRunning(userID))
	assert.Equal(t, 1, registry.Count())

	err := registry.DrainAll(5 * time.Second)
	require.NoError(t, err)
}

// TestRegistryDrainEmptyRegistry verifies that draining an empty registry
// completes immediately without error.
func TestRegistryDrainEmptyRegistry(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	registry := NewServiceRegistry(nil, cfg)

	start := time.Now()
	err := registry.DrainAll(5 * time.Second)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 1*time.Second, "draining empty registry should be near-instant")
}

// TestRegistryNilConfig verifies that NewServiceRegistry handles nil config
// by using DefaultConfig.
func TestRegistryNilConfig(t *testing.T) {
	t.Parallel()

	registry := NewServiceRegistry(nil, nil)
	assert.Equal(t, 0, registry.Count())
	// The registry should have created a default config internally.
	assert.NotNil(t, registry.cfg)
	assert.Equal(t, DefaultConfig().BootDelay, registry.cfg.BootDelay)
}

// TestBundleRunAndStopCleanly verifies that a standalone bundle starts
// its goroutines and shuts down cleanly when Stop is called.
func TestBundleRunAndStopCleanly(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bundle := NewUserServiceBundle(uuid.New(), uuid.New(), ctx, cancel, nil, cfg)

	done := make(chan struct{})
	go func() {
		bundle.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	bundle.Stop()

	select {
	case <-done:
		// Clean exit.
	case <-time.After(5 * time.Second):
		t.Fatal("bundle did not stop within 5 seconds")
	}
}
