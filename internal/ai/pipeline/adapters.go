// Package pipeline provides adapter types that bridge the queue.Pipeline
// interfaces with the concrete task engine implementations.
// This package exists to break the import cycle: queue -> tasks -> ai -> queue.
// The queue package defines interfaces (Categorizer, Summarizer, Drafter),
// this package adapts the concrete tasks.* engines to satisfy those interfaces.
package pipeline

import (
	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/ai/tasks"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// CategorizeAdapter wraps *tasks.CategorizeEngine to satisfy queue.Categorizer.
type CategorizeAdapter struct {
	Engine *tasks.CategorizeEngine
}

// CategorizeAndPrioritize delegates to the underlying engine, returning the
// result as interface{} to satisfy the queue.Categorizer interface.
func (a *CategorizeAdapter) CategorizeAndPrioritize(ctx database.TenantContext, email models.Email, userID uuid.UUID) (interface{}, error) {
	return a.Engine.CategorizeAndPrioritize(ctx, email, userID)
}

// SummarizeAdapter wraps *tasks.SummarizeEngine to satisfy queue.Summarizer.
type SummarizeAdapter struct {
	Engine *tasks.SummarizeEngine
}

// Summarize delegates to the underlying engine, returning the result as interface{}.
func (a *SummarizeAdapter) Summarize(ctx database.TenantContext, email models.Email, threadEmails []models.Email) (interface{}, error) {
	return a.Engine.Summarize(ctx, email, threadEmails)
}

// DraftAdapter wraps *tasks.DraftEngine to satisfy queue.Drafter.
type DraftAdapter struct {
	Engine *tasks.DraftEngine
}

// GenerateDraft delegates to the underlying engine. The catResult parameter
// is type-asserted back to *tasks.CategorizeResult from the opaque interface{}.
func (a *DraftAdapter) GenerateDraft(ctx database.TenantContext, email models.Email, tier string, catResult interface{}, userID uuid.UUID) (interface{}, error) {
	var cr *tasks.CategorizeResult
	if catResult != nil {
		cr, _ = catResult.(*tasks.CategorizeResult)
	}
	return a.Engine.GenerateDraft(ctx, email, tier, cr, userID)
}
