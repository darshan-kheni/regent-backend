package ai

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// ResponseCache provides Redis-backed caching for AI completion responses.
type ResponseCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewResponseCache creates a new cache with the given Redis client and TTL.
func NewResponseCache(client *redis.Client, ttl time.Duration) *ResponseCache {
	return &ResponseCache{client: client, ttl: ttl}
}

// cacheKey generates a deterministic cache key from model + messages.
func cacheKey(req CompletionRequest) string {
	h := sha256.New()
	h.Write([]byte(req.ModelID))
	msgJSON, _ := json.Marshal(req.Messages)
	h.Write(msgJSON)
	return fmt.Sprintf("ai:cache:%x", h.Sum(nil))
}

// Get checks the cache for a stored response.
func (c *ResponseCache) Get(ctx context.Context, req CompletionRequest) (*CompletionResponse, bool) {
	key := cacheKey(req)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}

	var resp CompletionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		slog.Warn("cache unmarshal error", "key", key, "error", err)
		return nil, false
	}

	resp.CacheHit = true
	return &resp, true
}

// Set stores a response in the cache.
func (c *ResponseCache) Set(ctx context.Context, req CompletionRequest, resp *CompletionResponse) {
	if resp.TokensOut == 0 {
		return // Don't cache empty/error responses.
	}

	key := cacheKey(req)
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Warn("cache marshal error", "key", key, "error", err)
		return
	}

	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		slog.Warn("cache set error", "key", key, "error", err)
	}
}
