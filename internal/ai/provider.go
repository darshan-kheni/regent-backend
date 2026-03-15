package ai

import (
	"context"
)

// AIProvider abstracts AI completion and embedding backends.
type AIProvider interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Embed(ctx context.Context, text string) ([]float32, error)
}

// CompletionRequest holds parameters for an AI completion call.
type CompletionRequest struct {
	ModelID     string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	Format      string         // "json" or "text"
	Options     map[string]any
}

// CompletionResponse holds the result of an AI completion call.
type CompletionResponse struct {
	Content   string
	TokensIn  int
	TokensOut int
	LatencyMs int64
	Model     string
	CacheHit  bool
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}
