package briefings

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RateLimiter enforces per-channel per-user hourly rate limits using Redis.
type RateLimiter struct {
	rdb *redis.Client
}

// channelLimits defines the maximum notifications per hour per user per channel.
var channelLimits = map[string]int{
	"sms":      10,
	"whatsapp": 20,
	"push":     50,
	"signal":   10,
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{rdb: rdb}
}

// Allow checks whether a notification can be sent on the given channel for this user.
// Uses Redis INCR with hourly bucket keys and TTL.
func (rl *RateLimiter) Allow(ctx context.Context, channel string, userID uuid.UUID) bool {
	if rl.rdb == nil {
		return true // No Redis — allow all
	}

	limit, ok := channelLimits[channel]
	if !ok {
		return true // Unknown channel — no limit
	}

	hourBucket := time.Now().Unix() / 3600
	key := fmt.Sprintf("ratelimit:%s:%s:%d", channel, userID, hourBucket)

	count, err := rl.rdb.Incr(ctx, key).Result()
	if err != nil {
		return true // Redis error — fail open
	}

	if count == 1 {
		rl.rdb.Expire(ctx, key, 2*time.Hour) // TTL with buffer
	}

	return int(count) <= limit
}
