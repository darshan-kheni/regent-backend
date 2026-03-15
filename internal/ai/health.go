package ai

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// HealthChecker monitors AI provider availability.
type HealthChecker struct {
	provider AIProvider
	name     string
	healthy  bool
	mu       sync.RWMutex
	stopCh   chan struct{}
}

// NewHealthChecker creates a new health checker for the given provider.
func NewHealthChecker(provider AIProvider, name string) *HealthChecker {
	return &HealthChecker{
		provider: provider,
		name:     name,
		stopCh:   make(chan struct{}),
	}
}

// CheckOnce runs a single health probe.
func (h *HealthChecker) CheckOnce(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := h.provider.Complete(ctx, CompletionRequest{
		ModelID:     "gemma3:4b",
		Messages:    []Message{{Role: "user", Content: "ping"}},
		Temperature: 0,
		MaxTokens:   1,
		Format:      "text",
	})

	h.mu.Lock()
	h.healthy = err == nil
	h.mu.Unlock()

	if err != nil {
		slog.Warn("health check failed", "provider", h.name, "error", err)
	} else {
		slog.Debug("health check passed", "provider", h.name)
	}

	return err == nil
}

// IsHealthy returns current health status.
func (h *HealthChecker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.healthy
}

// StartPeriodic runs health checks every interval in a background goroutine.
func (h *HealthChecker) StartPeriodic(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Initial check.
		h.CheckOnce(ctx)

		for {
			select {
			case <-ticker.C:
				h.CheckOnce(ctx)
			case <-h.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts periodic health checks.
func (h *HealthChecker) Stop() {
	close(h.stopCh)
}
