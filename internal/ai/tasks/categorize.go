package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/memory"
	"github.com/darshan-kheni/regent/internal/ai/prompts"
	"github.com/darshan-kheni/regent/internal/ai/rag"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// CategorizeResult is the combined categorization + priority JSON response.
type CategorizeResult struct {
	PrimaryCategory   string      `json:"primary_category"`
	Confidence        float64     `json:"confidence"`
	SecondaryCategory json.RawMessage `json:"secondary_category"`
	IsUrgent          bool        `json:"is_urgent"`
	PriorityScore     float64     `json:"priority_score"`
	PriorityFactors   []string    `json:"priority_factors"`
	Tone              string      `json:"tone"` // professional|warm_friendly|direct_concise|formal_legal|casual|urgent
	secondaryCat      string      // parsed from SecondaryCategory
}

// parsedSecondaryCategory extracts a string from SecondaryCategory which may be string, array, or null.
func (r *CategorizeResult) parsedSecondaryCategory() string {
	if len(r.SecondaryCategory) == 0 || string(r.SecondaryCategory) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(r.SecondaryCategory, &s) == nil {
		return s
	}
	var arr []string
	if json.Unmarshal(r.SecondaryCategory, &arr) == nil && len(arr) > 0 {
		return arr[0]
	}
	return ""
}

// PriorityScoreInt returns priority_score as an integer (0-100).
func (r *CategorizeResult) PriorityScoreInt() int {
	if r.PriorityScore <= 1.0 && r.PriorityScore > 0 {
		return int(r.PriorityScore * 100) // model returned 0.0-1.0 scale
	}
	return int(r.PriorityScore)
}

// GetCategory implements the interface used by pipeline filtering.
func (r *CategorizeResult) GetCategory() string { return r.PrimaryCategory }

// ValidTones are the 6 supported tone classifications for behavior intelligence.
var ValidTones = []string{
	"professional", "warm_friendly", "direct_concise", "formal_legal", "casual", "urgent",
}

// ValidCategories kept for backward compatibility with tests and priority scoring.
// The AI is no longer constrained to these — it can return any category.
var ValidCategories = []string{
	"Urgent", "Work", "Finance", "Legal", "Travel", "Personal", "Newsletter", "Spam",
}

// CategorizeEngine handles email categorization and priority scoring in a single batched AI call.
type CategorizeEngine struct {
	pool      *pgxpool.Pool
	provider  ai.AIProvider
	router    *ai.ModelRouter
	retriever *rag.Retriever
	builder   *prompts.PromptBuilder
	injector  *memory.ContextInjector
	rules     *UserRuleEngine
	rdb       *redis.Client
	audit     *AuditWriter
}

// NewCategorizeEngine creates a CategorizeEngine with all required dependencies.
func NewCategorizeEngine(
	pool *pgxpool.Pool,
	provider ai.AIProvider,
	router *ai.ModelRouter,
	retriever *rag.Retriever,
	builder *prompts.PromptBuilder,
	injector *memory.ContextInjector,
	ruleStore *memory.UserRuleStore,
	rdb *redis.Client,
) *CategorizeEngine {
	return &CategorizeEngine{
		pool:      pool,
		provider:  provider,
		router:    router,
		retriever: retriever,
		builder:   builder,
		injector:  injector,
		rules:     NewUserRuleEngine(ruleStore),
		rdb:       rdb,
		audit:     NewAuditWriter(pool),
	}
}

// CategorizeAndPrioritize performs batched categorization + priority scoring in a single AI call.
func (e *CategorizeEngine) CategorizeAndPrioritize(ctx database.TenantContext, email models.Email, userID uuid.UUID) (*CategorizeResult, error) {
	// Get model config for categorization (gemma3:4b)
	modelCfg := e.router.Route(ai.TaskCategorize, ai.EmailMeta{})

	// Retrieve RAG context
	ragCtx, _ := e.retriever.Retrieve(ctx, email, userID, ai.TaskCategorize)

	// Get memory context
	keywords := extractKeywords(email)
	memCtx, _ := e.injector.Inject(ctx, userID, "email", nil, keywords)

	// Build prompt
	emailInput := prompts.EmailInput{
		Subject:        email.Subject,
		FromAddress:    email.FromAddress,
		FromName:       email.FromName,
		BodyText:       email.BodyText,
		HasAttachments: email.HasAttachments,
	}
	// Load user AI preferences for personality injection
	var catUserConfig *prompts.UserPromptConfig
	if personality := LoadUserPersonality(ctx, e.pool, userID); personality != "" {
		catUserConfig = &prompts.UserPromptConfig{PersonalitySummary: personality}
	}
	messages, promptVersion, err := e.builder.Build(ai.TaskCategorize, emailInput, ragCtx, catUserConfig, memCtx, userID)
	if err != nil {
		return nil, fmt.Errorf("building categorize prompt: %w", err)
	}

	// Make AI call with model timeout
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
		return nil, fmt.Errorf("categorize AI call: %w", err)
	}

	// Parse JSON response (extract JSON from potential markdown/prose wrapping)
	cleaned := extractJSON(resp.Content)
	slog.Debug("categorize: raw AI response", "raw_len", len(resp.Content), "cleaned_len", len(cleaned), "cleaned_preview", cleaned[:min(200, len(cleaned))])
	var result CategorizeResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		slog.Error("categorize: JSON parse failed", "raw", resp.Content[:min(300, len(resp.Content))], "cleaned", cleaned[:min(300, len(cleaned))])
		return nil, fmt.Errorf("parsing categorize response: %w", err)
	}

	// Log token usage
	e.audit.Log(ctx, email.ID, "categorize", resp.Model, resp.TokensIn, resp.TokensOut, resp.LatencyMs, resp.CacheHit)

	// Normalize category: lowercase, trim whitespace, replace spaces with hyphens
	result.PrimaryCategory = normalizeCategory(result.PrimaryCategory)
	if result.PrimaryCategory == "" {
		result.PrimaryCategory = "uncategorized"
	}
	result.secondaryCat = normalizeCategory(result.parsedSecondaryCategory())

	// Normalize priority score (model may return 0.0-1.0 or 0-100)
	result.PriorityScore = float64(result.PriorityScoreInt())

	// If AI flagged urgent, boost priority
	if result.IsUrgent && result.PriorityScoreInt() < 80 {
		result.PriorityScore = 80
		result.PriorityFactors = append(result.PriorityFactors, "ai_urgent_flag")
	}

	// Apply user rule overrides
	result = e.rules.ApplyOverrides(ctx, userID, email, result)

	// Validate tone classification
	if result.Tone != "" && !isValidTone(result.Tone) {
		slog.Warn("AI returned invalid tone, defaulting to professional",
			"tone", result.Tone, "email_id", email.ID)
		result.Tone = "professional"
	}

	// Persist tone classification to emails table for behavior intelligence
	if result.Tone != "" {
		if toneErr := e.storeToneClassification(ctx, email.ID, result.Tone); toneErr != nil {
			slog.Warn("failed to store tone classification", "error", toneErr, "email_id", email.ID)
		}
	}

	// Publish briefing event to Redis Stream for dispatcher
	if result.PriorityScore > 0 {
		e.publishBriefingEvent(ctx, email, userID, result)
	}

	// Store result
	if err := e.storeResult(ctx, email.ID, result, resp.Model, promptVersion); err != nil {
		slog.Warn("failed to store categorize result", "error", err)
	}

	return &result, nil
}

func (e *CategorizeEngine) storeResult(ctx database.TenantContext, emailID uuid.UUID, result CategorizeResult, model string, _ int) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO email_categories (tenant_id, email_id, category, confidence, model_used, primary_category, secondary_category, priority_score)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (email_id) DO UPDATE SET
			category = EXCLUDED.category, confidence = EXCLUDED.confidence,
			model_used = EXCLUDED.model_used, primary_category = EXCLUDED.primary_category,
			secondary_category = EXCLUDED.secondary_category, priority_score = EXCLUDED.priority_score`,
		ctx.TenantID, emailID, result.PrimaryCategory, result.Confidence, model,
		result.PrimaryCategory, result.secondaryCat, result.PriorityScoreInt(),
	)
	return err
}

func isValidTone(tone string) bool {
	for _, valid := range ValidTones {
		if tone == valid {
			return true
		}
	}
	return false
}

func (e *CategorizeEngine) storeToneClassification(ctx database.TenantContext, emailID uuid.UUID, tone string) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`UPDATE emails SET tone_classification = $1 WHERE id = $2`,
		tone, emailID,
	)
	return err
}

// normalizeCategory lowercases and sanitizes an AI-generated category name.
func normalizeCategory(cat string) string {
	cat = strings.TrimSpace(strings.ToLower(cat))
	// Replace spaces with hyphens for URL-friendliness
	cat = strings.ReplaceAll(cat, " ", "-")
	// Remove any characters that aren't letters, numbers, or hyphens
	var cleaned []byte
	for i := range len(cat) {
		c := cat[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			cleaned = append(cleaned, c)
		}
	}
	return string(cleaned)
}

func extractKeywords(email models.Email) []string {
	// Simple keyword extraction from subject
	keywords := []string{email.FromAddress}
	if email.Subject != "" {
		keywords = append(keywords, email.Subject)
	}
	return keywords
}

// publishBriefingEvent sends a notification event to the Redis Stream for the BriefingDispatcher.
func (e *CategorizeEngine) publishBriefingEvent(ctx database.TenantContext, email models.Email, userID uuid.UUID, result CategorizeResult) {
	if e.rdb == nil {
		return
	}
	err := e.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "notification_events",
		MaxLen: 10000,
		Approx: true,
		Values: map[string]interface{}{
			"user_id":   userID.String(),
			"tenant_id": ctx.TenantID.String(),
			"email_id":  email.ID.String(),
			"priority":  result.PriorityScore,
			"category":  result.PrimaryCategory,
			"sender":    email.FromName,
			"subject":   email.Subject,
			"summary":   "",
		},
	}).Err()
	if err != nil {
		slog.Error("failed to publish briefing event",
			"email_id", email.ID,
			"error", err,
		)
	}
}
