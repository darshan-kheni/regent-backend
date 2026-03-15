package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/memory"
	"github.com/darshan-kheni/regent/internal/ai/prompts"
	"github.com/darshan-kheni/regent/internal/ai/rag"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// DraftResult is the AI-generated draft reply output.
type DraftResult struct {
	Variants     []DraftVariant `json:"variants"`
	ToneAnalysis string         `json:"tone_analysis,omitempty"` // premium only
}

// DraftVariant is one of the 3 reply variants.
type DraftVariant struct {
	Type    string `json:"type"`    // "concise", "detailed", "decline"
	Content string `json:"content"`
	Tone    string `json:"tone"`
}

// DraftEngine generates AI draft replies for emails.
type DraftEngine struct {
	pool        *pgxpool.Pool
	provider    ai.AIProvider
	router      *ai.ModelRouter
	retriever   *rag.Retriever
	builder     *prompts.PromptBuilder
	injector    *memory.ContextInjector
	sensitivity *SensitivityScorer
	style       *StyleExtractor
	audit       *AuditWriter
}

func NewDraftEngine(
	pool *pgxpool.Pool,
	provider ai.AIProvider,
	router *ai.ModelRouter,
	retriever *rag.Retriever,
	builder *prompts.PromptBuilder,
	injector *memory.ContextInjector,
) *DraftEngine {
	return &DraftEngine{
		pool:        pool,
		provider:    provider,
		router:      router,
		retriever:   retriever,
		builder:     builder,
		injector:    injector,
		sensitivity: NewSensitivityScorer(),
		style:       NewStyleExtractor(),
		audit:       NewAuditWriter(pool),
	}
}

// GenerateDraft creates 3 reply variants. tier: "quality", "premium", or "auto".
func (e *DraftEngine) GenerateDraft(ctx database.TenantContext, email models.Email, tier string, catResult *CategorizeResult, userID uuid.UUID) (*DraftResult, error) {
	// Determine actual tier
	actualTier := tier
	if tier == "auto" && catResult != nil {
		if e.sensitivity.ShouldUpgrade(email, catResult) {
			actualTier = "premium"
		} else {
			actualTier = "quality"
		}
	}

	// Select task type and model
	taskType := ai.TaskDraftReply
	if actualTier == "premium" {
		taskType = ai.TaskPremiumDraft
	}

	meta := ai.EmailMeta{
		Priority:    0,
		Category:    "",
		SenderIsVIP: false,
	}
	if catResult != nil {
		meta.Priority = catResult.PriorityScoreInt()
		meta.Category = catResult.PrimaryCategory
	}
	modelCfg := e.router.Route(taskType, meta)

	// Retrieve RAG context (past replies to sender + same category)
	ragCtx, _ := e.retriever.Retrieve(ctx, email, userID, taskType)

	// Extract style from past replies
	styleProfile := e.style.ExtractStyle(ragCtx)

	// Get memory context
	memCtx, _ := e.injector.Inject(ctx, userID, "email", nil, []string{email.Subject, email.FromAddress})

	// Build user config with style guidance + saved AI preferences
	personality := LoadUserPersonality(ctx, e.pool, userID)
	if styleProfile.Description != "" {
		if personality != "" {
			personality = styleProfile.Description + ". " + personality
		} else {
			personality = styleProfile.Description
		}
	}
	var userConfig *prompts.UserPromptConfig
	if personality != "" {
		userConfig = &prompts.UserPromptConfig{
			PersonalitySummary: personality,
		}
	}

	// Build prompt
	emailInput := prompts.EmailInput{
		Subject:        email.Subject,
		FromAddress:    email.FromAddress,
		FromName:       email.FromName,
		BodyText:       email.BodyText,
		HasAttachments: email.HasAttachments,
	}

	messages, promptVersion, err := e.builder.Build(taskType, emailInput, ragCtx, userConfig, memCtx, userID)
	if err != nil {
		return nil, fmt.Errorf("building draft prompt: %w", err)
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
		return nil, fmt.Errorf("draft AI call: %w", err)
	}

	// Log token usage
	e.audit.Log(ctx, email.ID, "draft_reply", resp.Model, resp.TokensIn, resp.TokensOut, resp.LatencyMs, resp.CacheHit)

	// Parse response (extract JSON from potential markdown/prose wrapping)
	var result DraftResult
	if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &result); err != nil {
		return nil, fmt.Errorf("parsing draft response: %w", err)
	}

	// Validate we got variants
	if len(result.Variants) == 0 {
		return nil, fmt.Errorf("AI returned no draft variants")
	}

	// Store only the single best draft
	isPremium := actualTier == "premium"
	// Confidence based on model tier: premium models produce higher quality drafts
	confidence := 0.75
	if isPremium {
		confidence = 0.90
	}
	if len(result.Variants) > 0 {
		if err := e.storeDraft(ctx, email.ID, userID, result.Variants[0], resp.Model, isPremium, confidence, promptVersion); err != nil {
			slog.Warn("failed to store draft", "error", err)
		}
	}

	return &result, nil
}

func (e *DraftEngine) storeDraft(ctx database.TenantContext, emailID, userID uuid.UUID, variant DraftVariant, model string, isPremium bool, confidence float64, promptVersion int) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO draft_replies (tenant_id, email_id, user_id, body, content, variant, model_used, is_premium, confidence, status, prompt_version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10)`,
		ctx.TenantID, emailID, userID, variant.Content, variant.Content, variant.Type,
		model, isPremium, confidence, fmt.Sprintf("v%d", promptVersion),
	)
	return err
}
