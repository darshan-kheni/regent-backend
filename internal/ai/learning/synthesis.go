package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/database"
)

// SynthesisResult is the output of the weekly preference synthesis.
type SynthesisResult struct {
	PersonalitySummary string        `json:"personality_summary"`
	FewShotExamples    []FewShotPair `json:"few_shot_examples"`
}

// FewShotPair represents an input/output example for few-shot prompting.
type FewShotPair struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// Synthesizer runs weekly preference synthesis per user.
type Synthesizer struct {
	pool     *pgxpool.Pool
	provider ai.AIProvider
	signals  *SignalStore
}

// NewSynthesizer creates a new Synthesizer.
func NewSynthesizer(pool *pgxpool.Pool, provider ai.AIProvider, signals *SignalStore) *Synthesizer {
	return &Synthesizer{pool: pool, provider: provider, signals: signals}
}

// RunWeeklySynthesis generates updated personality model from 7 days of signals.
// Uses gpt-oss:120b, 1 call per user per week. Scheduled Mon 6 AM.
func (s *Synthesizer) RunWeeklySynthesis(ctx database.TenantContext, userID uuid.UUID) error {
	since := time.Now().AddDate(0, 0, -7)
	signals, err := s.signals.GetRecent(ctx, userID, since)
	if err != nil {
		return fmt.Errorf("fetching recent signals: %w", err)
	}

	if len(signals) == 0 {
		slog.Debug("no signals for weekly synthesis", "user_id", userID)
		return nil
	}

	// Build synthesis prompt
	signalsJSON, _ := json.Marshal(signals)
	messages := []ai.Message{
		{Role: "system", Content: `You are analyzing a user's email preferences based on their corrections and overrides from the past week.
Generate a personality summary and few-shot examples that capture their communication style, priorities, and preferences.
Respond with JSON: {"personality_summary": "...", "few_shot_examples": [{"input": "...", "output": "..."}]}`},
		{Role: "user", Content: fmt.Sprintf("User corrections from the past 7 days:\n%s\n\nGenerate an updated personality profile and 3-5 few-shot examples.", string(signalsJSON))},
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.provider.Complete(reqCtx, ai.CompletionRequest{
		ModelID:     "gpt-oss:120b",
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   1000,
		Format:      "json",
	})
	if err != nil {
		return fmt.Errorf("synthesis AI call: %w", err)
	}

	var result SynthesisResult
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		return fmt.Errorf("parsing synthesis response: %w", err)
	}

	// Store in user_prompt_config
	fewShotJSON, _ := json.Marshal(result.FewShotExamples)
	return s.storeConfig(ctx, userID, result.PersonalitySummary, fewShotJSON)
}

func (s *Synthesizer) storeConfig(ctx database.TenantContext, userID uuid.UUID, personality string, fewShotJSON []byte) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO user_prompt_config (user_id, tenant_id, personality_summary, few_shot_examples, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (user_id) DO UPDATE SET
		   personality_summary = EXCLUDED.personality_summary,
		   few_shot_examples = EXCLUDED.few_shot_examples,
		   updated_at = now()`,
		userID, ctx.TenantID, personality, fewShotJSON,
	)
	if err != nil {
		return fmt.Errorf("storing prompt config: %w", err)
	}
	return nil
}
