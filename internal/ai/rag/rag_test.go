package rag

import (
	"context"
	"testing"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/models"
)

// mockProvider implements ai.AIProvider for testing.
type mockProvider struct {
	embedCalls  int
	embedResult []float32
}

func (m *mockProvider) Complete(_ context.Context, _ ai.CompletionRequest) (*ai.CompletionResponse, error) {
	return &ai.CompletionResponse{Content: "test"}, nil
}

func (m *mockProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	m.embedCalls++
	return m.embedResult, nil
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		max    int
		expect string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"exact max", "hello", 5, "hello"},
		{"longer than max", "hello world", 5, "hello"},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.max)
			if result != tt.expect {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expect)
			}
		})
	}
}

func TestBuildQueryText_TaskTypes(t *testing.T) {
	t.Parallel()
	// Create a long body text
	longBody := make([]byte, 3000)
	for i := range longBody {
		longBody[i] = 'a'
	}

	email := models.Email{
		Subject:     "Test Subject",
		FromAddress: "sender@example.com",
		BodyText:    string(longBody),
	}

	tests := []struct {
		task    ai.TaskType
		maxBody int // expected max body chars
	}{
		{ai.TaskCategorize, 500},
		{ai.TaskPrioritize, 500},
		{ai.TaskSummarize, 2000},
		{ai.TaskDraftReply, 1500},
		{ai.TaskPremiumDraft, 1500},
	}

	for _, tt := range tests {
		t.Run(string(tt.task), func(t *testing.T) {
			result := buildQueryText(email, tt.task)
			// Should contain subject
			if len(result) == 0 {
				t.Error("query text should not be empty")
			}
			// Body portion should be truncated appropriately
			maxExpected := len("Test Subject") + len(" sender@example.com ") + tt.maxBody + 10
			if len(result) > maxExpected {
				t.Errorf("query text too long for %s: got %d, max expected ~%d", tt.task, len(result), maxExpected)
			}
		})
	}
}

func TestEmbedder_CachesResults(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{embedResult: make([]float32, 768)}
	embedder := NewEmbedder(mock)

	ctx := context.Background()

	// First call should hit the provider
	_, err := embedder.Embed(ctx, "test text")
	if err != nil {
		t.Fatal(err)
	}
	if mock.embedCalls != 1 {
		t.Errorf("expected 1 embed call, got %d", mock.embedCalls)
	}

	// Second call with same text should be cached
	_, err = embedder.Embed(ctx, "test text")
	if err != nil {
		t.Fatal(err)
	}
	if mock.embedCalls != 1 {
		t.Errorf("expected 1 embed call (cached), got %d", mock.embedCalls)
	}

	// Different text should call provider again
	_, err = embedder.Embed(ctx, "different text")
	if err != nil {
		t.Fatal(err)
	}
	if mock.embedCalls != 2 {
		t.Errorf("expected 2 embed calls, got %d", mock.embedCalls)
	}
}

func TestEmbedder_ClearCache(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{embedResult: make([]float32, 768)}
	embedder := NewEmbedder(mock)

	ctx := context.Background()
	_, _ = embedder.Embed(ctx, "test")
	embedder.ClearCache()
	_, _ = embedder.Embed(ctx, "test")

	if mock.embedCalls != 2 {
		t.Errorf("after ClearCache, expected 2 calls, got %d", mock.embedCalls)
	}
}
