package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// Default supervisor constants (used by calculateBackoff and applyJitter helpers).
const (
	supervisorBackoffFactor = 2.0
	supervisorJitterMin     = 0.8
	supervisorJitterMax     = 1.2
)

// runWithSupervisor wraps a service function with restart-on-failure supervision.
//
// Behavior:
//   - On failure, retries with exponential backoff (configurable base, 2x multiplier, configurable cap).
//   - Jitter of +/-20% is applied to the backoff to avoid thundering herd.
//   - After cfg.SupervisorMaxFailures consecutive failures, the service is paused (dead-lettered).
//   - If the service runs stably for > cfg.SupervisorStableThreshold, consecutive failures and backoff are reset.
//   - On context cancellation, the supervisor exits immediately.
func (b *UserServiceBundle) runWithSupervisor(name string, fn func(context.Context) error) {
	baseBackoff := b.cfg.SupervisorBaseBackoff
	maxBackoff := b.cfg.SupervisorMaxBackoff
	maxFailures := b.cfg.SupervisorMaxFailures
	stableThreshold := b.cfg.SupervisorStableThreshold

	var (
		consecutiveFailures int
		backoff             = baseBackoff
	)

	for {
		// Check if context is already cancelled before starting.
		if b.ctx.Err() != nil {
			return
		}

		b.health.SetStatus(name, "running")
		startTime := time.Now()

		err := fn(b.ctx)

		// If context was cancelled, exit cleanly — not a failure.
		if b.ctx.Err() != nil {
			return
		}

		if err == nil {
			// Clean exit without context cancellation is unusual but not an error.
			slog.Info("supervisor: service exited cleanly, restarting",
				"service", name,
				"user_id", b.UserID,
			)
			consecutiveFailures = 0
			backoff = baseBackoff
			continue
		}

		// Determine if the service ran long enough to be considered stable.
		runDuration := time.Since(startTime)
		if runDuration >= stableThreshold {
			// Was stable before crashing — reset counters.
			consecutiveFailures = 0
			backoff = baseBackoff
		}

		consecutiveFailures++

		slog.Error("supervisor: service failed",
			"service", name,
			"user_id", b.UserID,
			"consecutive_failures", consecutiveFailures,
			"error", fmt.Errorf("service %s: %w", name, err),
		)

		// Dead letter: too many consecutive failures.
		if consecutiveFailures >= maxFailures {
			slog.Error("supervisor: dead-lettering service after max failures",
				"service", name,
				"user_id", b.UserID,
				"max_failures", maxFailures,
			)
			b.health.SetError(name, fmt.Sprintf("dead-lettered after %d consecutive failures: %v", consecutiveFailures, err))
			b.health.SetStatus(name, "paused")
			return
		}

		b.health.SetStatus(name, "error")

		// Apply jitter to backoff.
		jittered := applyJitter(backoff)

		slog.Info("supervisor: waiting before restart",
			"service", name,
			"user_id", b.UserID,
			"backoff", jittered,
		)

		select {
		case <-b.ctx.Done():
			return
		case <-time.After(jittered):
		}

		// Increase backoff for next iteration, capped at max.
		backoff = nextBackoffCapped(backoff, maxBackoff)
	}
}

// applyJitter applies +/-20% jitter to a duration.
func applyJitter(d time.Duration) time.Duration {
	jitter := supervisorJitterMin + rand.Float64()*(supervisorJitterMax-supervisorJitterMin)
	return time.Duration(float64(d) * jitter)
}

// nextBackoffCapped doubles the backoff, capped at max.
func nextBackoffCapped(current, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * supervisorBackoffFactor)
	if next > max {
		next = max
	}
	return next
}

// calculateBackoff computes the backoff for a given failure count (0-indexed),
// using the default production base of 2s. Exported for testing.
func calculateBackoff(failureCount int) time.Duration {
	base := float64(2 * time.Second)
	d := base * math.Pow(supervisorBackoffFactor, float64(failureCount))
	if time.Duration(d) > 5*time.Minute {
		return 5 * time.Minute
	}
	return time.Duration(d)
}
