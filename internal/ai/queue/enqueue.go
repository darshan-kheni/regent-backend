package queue

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Enqueuer abstracts job enqueueing for testability across packages.
type Enqueuer interface {
	EnqueueEmail(ctx context.Context, emailID, userID, tenantID uuid.UUID, plan string) error
}

// QueueEnqueuer implements the Enqueuer interface using the Redis-backed Queue.
type QueueEnqueuer struct {
	q *Queue
}

// NewQueueEnqueuer creates a new QueueEnqueuer wrapping the given Queue.
func NewQueueEnqueuer(q *Queue) *QueueEnqueuer {
	return &QueueEnqueuer{q: q}
}

// EnqueueEmail adds an email to the AI processing queue with plan-based priority.
func (qe *QueueEnqueuer) EnqueueEmail(ctx context.Context, emailID, userID, tenantID uuid.UUID, plan string) error {
	priority := PlanPriority[plan]
	if priority == 0 {
		priority = PlanPriority["free"]
	}

	return qe.q.Enqueue(ctx, Job{
		EmailID:    emailID,
		UserID:     userID,
		TenantID:   tenantID,
		Plan:       plan,
		Priority:   priority,
		EnqueuedAt: time.Now(),
	})
}
