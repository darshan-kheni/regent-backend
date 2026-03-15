package rag

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// ContextItem represents a RAG-retrieved context piece for prompt injection.
type ContextItem struct {
	SourceType     string
	SourceID       uuid.UUID
	ContentPreview string
	Similarity     float64
	Metadata       map[string]any
}

// Retriever fetches relevant context from the vector store for AI prompt injection.
type Retriever struct {
	store    *VectorStore
	embedder *Embedder
}

func NewRetriever(store *VectorStore, embedder *Embedder) *Retriever {
	return &Retriever{store: store, embedder: embedder}
}

// Retrieve fetches context items relevant to the given email and task type.
func (r *Retriever) Retrieve(ctx database.TenantContext, email models.Email, userID uuid.UUID, task ai.TaskType) ([]ContextItem, error) {
	queryText := buildQueryText(email, task)

	embedding, err := r.embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embedding query for retrieval: %w", err)
	}

	results, err := r.store.Search(ctx, embedding, userID, 0.65, 5)
	if err != nil {
		return nil, fmt.Errorf("searching vector store: %w", err)
	}

	items := make([]ContextItem, len(results))
	for i, res := range results {
		items[i] = ContextItem{
			SourceType:     res.SourceType,
			SourceID:       res.SourceID,
			ContentPreview: res.ContentPreview,
			Similarity:     res.Similarity,
			Metadata:       res.Metadata,
		}
	}
	return items, nil
}

// buildQueryText constructs the text to embed for RAG retrieval, truncated per task type.
func buildQueryText(email models.Email, task ai.TaskType) string {
	switch task {
	case ai.TaskCategorize, ai.TaskPrioritize:
		// Categorization: subject + sender + first 500 chars
		body := truncate(email.BodyText, 500)
		return fmt.Sprintf("%s %s %s", email.Subject, email.FromAddress, body)
	case ai.TaskSummarize:
		// Summarization: subject + first 2000 chars
		body := truncate(email.BodyText, 2000)
		return fmt.Sprintf("%s %s", email.Subject, body)
	case ai.TaskDraftReply, ai.TaskPremiumDraft:
		// Draft reply: subject + sender + first 1500 chars
		body := truncate(email.BodyText, 1500)
		return fmt.Sprintf("%s %s %s", email.Subject, email.FromAddress, body)
	default:
		body := truncate(email.BodyText, 500)
		return fmt.Sprintf("%s %s", email.Subject, body)
	}
}

// truncate cuts text to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
