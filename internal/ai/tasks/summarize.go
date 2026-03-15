package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/memory"
	"github.com/darshan-kheni/regent/internal/ai/prompts"
	"github.com/darshan-kheni/regent/internal/ai/rag"
	"github.com/darshan-kheni/regent/internal/calendar"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// SummaryResult is the structured summary output from the AI.
type SummaryResult struct {
	Headline          string                       `json:"headline"`
	KeyPoints         []string                     `json:"key_points"`
	ActionRequired    bool                         `json:"action_required"`
	ActionDescription string                       `json:"action_description"`
	UrgencyHint       string                       `json:"urgency_hint"`
	Scheduling        *calendar.SchedulingAnalysis `json:"scheduling,omitempty"`
	Tasks             []AIExtractedTask            `json:"tasks,omitempty"`
}

// GetActionRequired implements the interface used by pipeline filtering.
func (r *SummaryResult) GetActionRequired() bool {
	if r == nil {
		return false
	}
	return r.ActionRequired
}

// AIExtractedTask represents a raw task extracted by the AI from an email.
// Defined here to avoid circular import with internal/tasks package.
type AIExtractedTask struct {
	Description  string  `json:"description"`
	Type         string  `json:"type"`
	DeadlineText string  `json:"deadline_text"`
	Assignee     string  `json:"assignee_email"`
	PriorityHint string  `json:"priority_hint"`
	Confidence   float64 `json:"confidence"`
}

// SummarizeEngine generates AI summaries for emails.
type SummarizeEngine struct {
	pool      *pgxpool.Pool
	provider  ai.AIProvider
	router    *ai.ModelRouter
	retriever *rag.Retriever
	builder   *prompts.PromptBuilder
	injector  *memory.ContextInjector
	rdb       *redis.Client
	audit     *AuditWriter
}

func NewSummarizeEngine(
	pool *pgxpool.Pool,
	provider ai.AIProvider,
	router *ai.ModelRouter,
	retriever *rag.Retriever,
	builder *prompts.PromptBuilder,
	injector *memory.ContextInjector,
	rdb *redis.Client,
) *SummarizeEngine {
	return &SummarizeEngine{
		pool:      pool,
		provider:  provider,
		router:    router,
		retriever: retriever,
		builder:   builder,
		injector:  injector,
		rdb:       rdb,
		audit:     NewAuditWriter(pool),
	}
}

// Summarize generates a structured summary for an email, with thread awareness and Redis caching.
func (e *SummarizeEngine) Summarize(ctx database.TenantContext, email models.Email, threadEmails []models.Email) (*SummaryResult, error) {
	// Check Redis cache first
	cacheKey := summaryCacheKey(email.BodyText)
	if cached, err := e.getCached(ctx, cacheKey); err == nil && cached != nil {
		return cached, nil
	}

	// Get model config
	modelCfg := e.router.Route(ai.TaskSummarize, ai.EmailMeta{})

	// Retrieve RAG context
	ragCtx, _ := e.retriever.Retrieve(ctx, email, email.UserID, ai.TaskSummarize)

	// Get memory context
	memCtx, _ := e.injector.Inject(ctx, email.UserID, "email", nil, []string{email.Subject, email.FromAddress})

	// Build thread context if reply chain
	var threadContext string
	if len(threadEmails) > 1 {
		threadContext = BuildThreadContext(threadEmails)
	}

	// Build prompt
	bodyText := email.BodyText
	if threadContext != "" {
		bodyText = fmt.Sprintf("[THREAD CONTEXT]\n%s\n\n[NEW MESSAGE]\n%s", threadContext, email.BodyText)
	}

	emailInput := prompts.EmailInput{
		Subject:        email.Subject,
		FromAddress:    email.FromAddress,
		FromName:       email.FromName,
		BodyText:       bodyText,
		HasAttachments: email.HasAttachments,
	}

	// Load user AI preferences for personality injection
	var sumUserConfig *prompts.UserPromptConfig
	if personality := LoadUserPersonality(ctx, e.pool, email.UserID); personality != "" {
		sumUserConfig = &prompts.UserPromptConfig{PersonalitySummary: personality}
	}
	messages, _, err := e.builder.Build(ai.TaskSummarize, emailInput, ragCtx, sumUserConfig, memCtx, email.UserID)
	if err != nil {
		return nil, fmt.Errorf("building summarize prompt: %w", err)
	}

	// Make AI call
	reqCtx, cancel := context.WithTimeout(ctx, modelCfg.Timeout)
	defer cancel()

	resp, err := e.provider.Complete(reqCtx, ai.CompletionRequest{
		ModelID:     modelCfg.ModelID,
		Messages:    messages,
		Temperature: modelCfg.Temperature,
		MaxTokens:   modelCfg.MaxTokens,
		Format:      "json",
		Options:     map[string]any{"timeout": modelCfg.Timeout},
	})
	if err != nil {
		return nil, fmt.Errorf("summarize AI call: %w", err)
	}

	// Log token usage
	e.audit.Log(ctx, email.ID, "summarize", resp.Model, resp.TokensIn, resp.TokensOut, resp.LatencyMs, resp.CacheHit)

	// Parse response (extract JSON from potential markdown/prose wrapping)
	var result SummaryResult
	if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &result); err != nil {
		return nil, fmt.Errorf("parsing summary response: %w", err)
	}

	// Note attachments if present
	if email.HasAttachments {
		result.KeyPoints = append(result.KeyPoints, "Email includes attachments")
	}

	// Cache in Redis
	e.setCache(ctx, cacheKey, &result)

	// Store in database
	if err := e.storeResult(ctx, email.ID, result, resp.Model); err != nil {
		slog.Warn("failed to store summary", "error", err)
	}

	return &result, nil
}

func summaryCacheKey(bodyText string) string {
	h := sha256.Sum256([]byte(bodyText))
	return fmt.Sprintf("ai:summary:%x", h)
}

func (e *SummarizeEngine) getCached(ctx context.Context, key string) (*SummaryResult, error) {
	if e.rdb == nil {
		return nil, fmt.Errorf("no redis")
	}
	data, err := e.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var result SummaryResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (e *SummarizeEngine) setCache(ctx context.Context, key string, result *SummaryResult) {
	if e.rdb == nil {
		return
	}
	data, err := json.Marshal(result)
	if err != nil {
		return
	}
	e.rdb.Set(ctx, key, data, 24*time.Hour)
}

func (e *SummarizeEngine) storeResult(ctx database.TenantContext, emailID uuid.UUID, result SummaryResult, model string) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	keyPointsJSON, _ := json.Marshal(result.KeyPoints)
	summaryText := result.Headline
	if result.ActionDescription != "" {
		summaryText += " | Action: " + result.ActionDescription
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO email_summaries (tenant_id, email_id, summary, model_used, headline, key_points, action_required)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ctx.TenantID, emailID, summaryText, model,
		result.Headline, keyPointsJSON, result.ActionRequired,
	)
	return err
}
