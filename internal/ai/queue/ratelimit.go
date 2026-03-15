package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a sliding window rate limiter using Redis sorted sets.
type RateLimiter struct {
	rdb       *redis.Client
	maxPerMin int
	burstMax  int
}

// NewRateLimiter creates a new RateLimiter with the given per-minute and burst limits.
func NewRateLimiter(rdb *redis.Client, maxPerMin, burstMax int) *RateLimiter {
	return &RateLimiter{
		rdb:       rdb,
		maxPerMin: maxPerMin,
		burstMax:  burstMax,
	}
}

// Allow checks if an AI request is allowed under rate limits.
// Uses a sliding window counter with a Redis sorted set.
// Fails open on Redis errors (returns true).
func (rl *RateLimiter) Allow(ctx context.Context) (bool, error) {
	key := "ai_ratelimit"
	now := time.Now()
	windowStart := now.Add(-1 * time.Minute)

	pipe := rl.rdb.Pipeline()
	// Remove entries outside the window
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.UnixNano()))
	// Count entries in window
	countCmd := pipe.ZCard(ctx, key)
	_, err := pipe.Exec(ctx)
	if err != nil {
		// Fail open on Redis errors
		return true, nil
	}

	count := countCmd.Val()
	if count >= int64(rl.burstMax) {
		return false, nil
	}

	// Add current request
	rl.rdb.ZAdd(ctx, key, redis.Z{
		Score:  float64(now.UnixNano()),
		Member: fmt.Sprintf("%d", now.UnixNano()),
	})
	rl.rdb.Expire(ctx, key, 2*time.Minute) // TTL to prevent unbounded growth

	return true, nil
}

// WaitForSlot blocks until a rate limit slot is available or context is cancelled.
func (rl *RateLimiter) WaitForSlot(ctx context.Context) error {
	for {
		allowed, err := rl.Allow(ctx)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
