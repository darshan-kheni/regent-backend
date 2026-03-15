package rag

import (
	"context"
	"fmt"
	"sync"

	"github.com/darshan-kheni/regent/internal/ai"
)

// Embedder wraps the AIProvider.Embed method with per-request caching
// to avoid re-embedding the same text for different pipeline stages.
type Embedder struct {
	provider ai.AIProvider
	cache    map[string][]float32
	mu       sync.RWMutex
}

func NewEmbedder(provider ai.AIProvider) *Embedder {
	return &Embedder{
		provider: provider,
		cache:    make(map[string][]float32),
	}
}

// Embed returns a 768-dim embedding, using a cache to avoid duplicate calls.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	if vec, ok := e.cache[text]; ok {
		e.mu.RUnlock()
		return vec, nil
	}
	e.mu.RUnlock()

	vec, err := e.provider.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embedding text: %w", err)
	}

	e.mu.Lock()
	e.cache[text] = vec
	e.mu.Unlock()

	return vec, nil
}

// ClearCache resets the per-request embedding cache.
func (e *Embedder) ClearCache() {
	e.mu.Lock()
	e.cache = make(map[string][]float32)
	e.mu.Unlock()
}
