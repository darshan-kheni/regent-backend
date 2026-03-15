package connection

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"
)

// Reconnector handles exponential backoff reconnection with jitter.
// Backoff: 2s -> 4s -> 8s -> ... -> 5min max.
// Jitter: +/-20% to prevent thundering herd.
type Reconnector struct {
	manager     *ConnectionManager
	baseBackoff time.Duration
	maxBackoff  time.Duration
	maxAttempts int
}

// NewReconnector creates a Reconnector with default settings.
func NewReconnector(manager *ConnectionManager) *Reconnector {
	return &Reconnector{
		manager:     manager,
		baseBackoff: 2 * time.Second,
		maxBackoff:  5 * time.Minute,
		maxAttempts: 10,
	}
}

// Reconnect attempts to re-establish a connection with exponential backoff.
// Returns nil on success or the last error after exhausting all attempts.
func (r *Reconnector) Reconnect(ctx context.Context, accountID uuid.UUID, dialFn func() (*imapclient.Client, error)) error {
	backoff := r.baseBackoff

	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		client, err := dialFn()
		if err == nil {
			r.manager.Remove(accountID) // Remove old errored connection
			if regErr := r.manager.Register(accountID, client); regErr != nil {
				client.Close()
				return fmt.Errorf("re-register connection: %w", regErr)
			}
			slog.Info("IMAP reconnection successful", "account_id", accountID, "attempt", attempt)
			return nil
		}

		// Calculate jitter: +/-20%
		jitter := applyJitter(backoff)

		slog.Warn("IMAP reconnect failed, retrying",
			"account_id", accountID,
			"attempt", attempt,
			"max_attempts", r.maxAttempts,
			"backoff", jitter,
			"error", err,
		)

		select {
		case <-time.After(jitter):
			backoff = min(backoff*2, r.maxBackoff)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("reconnect failed after %d attempts for account %s", r.maxAttempts, accountID)
}

// applyJitter applies +/-20% jitter to the given duration.
func applyJitter(d time.Duration) time.Duration {
	// Range: 0.8x to 1.2x
	factor := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * factor)
}

// CalculateBackoff returns the backoff duration for a given attempt (0-indexed).
// Exported for testing.
func CalculateBackoff(attempt int, base, max time.Duration) time.Duration {
	backoff := base
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff > max {
			return max
		}
	}
	return backoff
}
