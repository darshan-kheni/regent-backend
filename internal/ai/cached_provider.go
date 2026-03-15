package ai

import (
	"context"
)

// CachedProvider wraps an AIProvider with a ResponseCache.
type CachedProvider struct {
	provider AIProvider
	cache    *ResponseCache
}

// NewCachedProvider creates a new provider that checks the cache before calling the underlying provider.
func NewCachedProvider(provider AIProvider, cache *ResponseCache) *CachedProvider {
	return &CachedProvider{provider: provider, cache: cache}
}

// Complete checks the cache first, then calls the underlying provider on miss.
func (c *CachedProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	// Check cache first.
	if resp, ok := c.cache.Get(ctx, req); ok {
		return resp, nil
	}

	// Call provider.
	resp, err := c.provider.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	// Store in cache.
	c.cache.Set(ctx, req, resp)
	return resp, nil
}

// Embed delegates directly to the underlying provider (embeddings are not cached).
func (c *CachedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return c.provider.Embed(ctx, text)
}
