package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sony/gobreaker/v2"
)

// RateLimitError indicates an HTTP 429 response, which should NOT trip the circuit breaker.
type RateLimitError struct {
	StatusCode int
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (HTTP %d): %s", e.StatusCode, e.Message)
}

// isRateLimitError checks if an error is a rate limit (429) that should not trip the breaker.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*RateLimitError); ok {
		return true
	}
	// Also check for status code in error message as fallback.
	return strings.Contains(err.Error(), "status 429")
}

// CircuitBreakerProvider wraps a primary AIProvider with circuit breaker logic,
// falling back to a secondary provider when the circuit is open.
type CircuitBreakerProvider struct {
	primary  AIProvider
	fallback AIProvider
	cb       *gobreaker.CircuitBreaker[*CompletionResponse]
	embedCB  *gobreaker.CircuitBreaker[[]float32]
}

// NewCircuitBreakerProvider creates a circuit breaker that trips after the given number of
// consecutive failures within the interval, and stays open for the timeout duration.
func NewCircuitBreakerProvider(primary, fallback AIProvider, failures int, interval, timeout time.Duration) *CircuitBreakerProvider {
	settings := gobreaker.Settings{
		Name:        "ollama-circuit-breaker",
		MaxRequests: 1,
		Interval:    interval,
		Timeout:     timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(failures)
		},
		IsSuccessful: func(err error) bool {
			// 429 is NOT a failure — don't trip the breaker.
			return err == nil || isRateLimitError(err)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Warn("circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	}

	embedSettings := settings
	embedSettings.Name = "ollama-embed-circuit-breaker"

	return &CircuitBreakerProvider{
		primary:  primary,
		fallback: fallback,
		cb:       gobreaker.NewCircuitBreaker[*CompletionResponse](settings),
		embedCB:  gobreaker.NewCircuitBreaker[[]float32](embedSettings),
	}
}

// Complete calls the primary provider, falling back to the secondary on failure.
func (c *CircuitBreakerProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	resp, err := c.cb.Execute(func() (*CompletionResponse, error) {
		return c.primary.Complete(ctx, req)
	})
	if err != nil {
		if isRateLimitError(err) {
			return nil, err
		}
		slog.Warn("primary provider failed, trying fallback",
			"error", err,
			"circuit_state", c.cb.State().String(),
		)
		return c.fallback.Complete(ctx, req)
	}
	return resp, nil
}

// Embed calls the primary provider only. Gemini cannot embed (dimension mismatch).
func (c *CircuitBreakerProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := c.embedCB.Execute(func() ([]float32, error) {
		return c.primary.Embed(ctx, text)
	})
	if err != nil {
		return nil, fmt.Errorf("embed failed (no fallback for embeddings): %w", err)
	}
	return result, nil
}

// State returns the current circuit breaker state.
func (c *CircuitBreakerProvider) State() gobreaker.State {
	return c.cb.State()
}
