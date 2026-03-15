package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Job represents an AI processing job for a single email.
type Job struct {
	EmailID    uuid.UUID `json:"email_id"`
	UserID     uuid.UUID `json:"user_id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	Plan       string    `json:"plan"`
	Priority   int       `json:"priority"` // higher = dequeued first (estate=40, privy=30, attache=20, free=10)
	EnqueuedAt time.Time `json:"enqueued_at"`
}

// PlanPriority maps plan names to queue priority values.
var PlanPriority = map[string]int{
	"estate":  40,
	"privy_council": 30,
	"attache": 20,
	"free":    10,
}

// Queue manages per-user Redis job queues with a priority meta-queue.
type Queue struct {
	rdb *redis.Client
}

// NewQueue creates a new Queue backed by the given Redis client.
func NewQueue(rdb *redis.Client) *Queue {
	return &Queue{rdb: rdb}
}

// Enqueue adds a job to the user's queue and updates the meta-queue sorted set.
// Meta-queue key: "ai_meta_queue", scored by plan priority (higher = dequeued first).
// Per-user queue key: "ai_queue:{user_id}"
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshaling job: %w", err)
	}

	pipe := q.rdb.Pipeline()
	userKey := fmt.Sprintf("ai_queue:%s", job.UserID)
	pipe.LPush(ctx, userKey, data)
	// Add user to meta-queue with priority score (higher priority users dequeued first)
	pipe.ZAdd(ctx, "ai_meta_queue", redis.Z{
		Score:  float64(job.Priority),
		Member: job.UserID.String(),
	})
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("enqueuing job: %w", err)
	}
	return nil
}

// Dequeue gets the next job using fair scheduling.
// Picks the highest-priority user from meta-queue, then RPOP from their queue.
// If their queue is empty after pop, removes them from the meta-queue.
// Returns nil, nil if no jobs available.
func (q *Queue) Dequeue(ctx context.Context) (*Job, error) {
	// Get highest-priority user from sorted set (highest score first)
	members, err := q.rdb.ZRevRangeWithScores(ctx, "ai_meta_queue", 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("reading meta-queue: %w", err)
	}
	if len(members) == 0 {
		return nil, nil
	}

	userIDStr := members[0].Member.(string)
	userKey := fmt.Sprintf("ai_queue:%s", userIDStr)

	// RPOP from this user's queue
	data, err := q.rdb.RPop(ctx, userKey).Bytes()
	if err == redis.Nil {
		// Queue empty, remove from meta-queue
		q.rdb.ZRem(ctx, "ai_meta_queue", userIDStr)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dequeuing job: %w", err)
	}

	// Check if queue is now empty, remove from meta-queue if so
	remaining, _ := q.rdb.LLen(ctx, userKey).Result()
	if remaining == 0 {
		q.rdb.ZRem(ctx, "ai_meta_queue", userIDStr)
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshaling job: %w", err)
	}
	return &job, nil
}

// QueueLength returns the number of pending jobs for a user.
func (q *Queue) QueueLength(ctx context.Context, userID uuid.UUID) (int64, error) {
	return q.rdb.LLen(ctx, fmt.Sprintf("ai_queue:%s", userID)).Result()
}
