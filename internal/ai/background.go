package ai

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// AIProcessor is a per-user component that monitors for new emails
// and enqueues them into the global AI processing queue.
// It runs as a supervised goroutine within each UserServiceBundle.
type AIProcessor struct {
	userID   uuid.UUID
	tenantID uuid.UUID
	plan     string
	enqueuer Enqueuer
}

// Enqueuer abstracts the job queue for testability.
type Enqueuer interface {
	EnqueueEmail(ctx context.Context, emailID, userID, tenantID uuid.UUID, plan string) error
}

// NewAIProcessor creates a new per-user AI processor.
func NewAIProcessor(userID, tenantID uuid.UUID, plan string, enqueuer Enqueuer) *AIProcessor {
	return &AIProcessor{
		userID:   userID,
		tenantID: tenantID,
		plan:     plan,
		enqueuer: enqueuer,
	}
}

// Run blocks until context is cancelled. The AIProcessor itself is passive —
// emails are enqueued by the realtime layer (idle.go/dispatcher.go) and
// processed by the global WorkerPool. This goroutine exists to:
// 1. Report AI status as "running" to the health system
// 2. Allow the supervisor to restart it if it crashes
// 3. Hold per-user AI state (plan, config)
func (ap *AIProcessor) Run(ctx context.Context) error {
	slog.Info("ai processor started",
		"user_id", ap.userID,
		"plan", ap.plan,
	)
	<-ctx.Done()
	slog.Info("ai processor stopped", "user_id", ap.userID)
	return nil
}

// UpdatePlan hot-reloads the plan config without restart.
func (ap *AIProcessor) UpdatePlan(plan string) {
	ap.plan = plan
	slog.Info("ai processor: plan updated",
		"user_id", ap.userID,
		"plan", plan,
	)
}

// Plan returns the current plan.
func (ap *AIProcessor) Plan() string {
	return ap.plan
}
