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

// QuickUpdater performs lightweight daily prompt config updates.
type QuickUpdater struct {
	pool     *pgxpool.Pool
	provider ai.AIProvider
	signals  *SignalStore
}

// NewQuickUpdater creates a new QuickUpdater.
func NewQuickUpdater(pool *pgxpool.Pool, provider ai.AIProvider, signals *SignalStore) *QuickUpdater {
	return &QuickUpdater{pool: pool, provider: provider, signals: signals}
}

// RunIfNeeded checks if >5 corrections today and runs a quick update.
func (qu *QuickUpdater) RunIfNeeded(ctx database.TenantContext, userID uuid.UUID) error {
	count, err := qu.signals.CountToday(ctx, userID)
	if err != nil {
		return fmt.Errorf("counting today's signals: %w", err)
	}

	if count <= 5 {
		return nil // not enough corrections to trigger
	}

	slog.Info("triggering daily quick update", "user_id", userID, "corrections", count)

	since := time.Now().Truncate(24 * time.Hour)
	signals, err := qu.signals.GetRecent(ctx, userID, since)
	if err != nil {
		return fmt.Errorf("fetching today's signals: %w", err)
	}

	signalsJSON, _ := json.Marshal(signals)
	messages := []ai.Message{
		{Role: "system", Content: `Analyze today's user corrections and generate 2-3 new few-shot examples that capture the patterns.
Respond with JSON: {"few_shot_examples": [{"input": "...", "output": "..."}]}`},
		{Role: "user", Content: fmt.Sprintf("Today's corrections:\n%s", string(signalsJSON))},
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := qu.provider.Complete(reqCtx, ai.CompletionRequest{
		ModelID:     "ministral-3:8b",
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   500,
		Format:      "json",
	})
	if err != nil {
		return fmt.Errorf("quick update AI call: %w", err)
	}

	var result struct {
		FewShotExamples []FewShotPair `json:"few_shot_examples"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		return fmt.Errorf("parsing quick update: %w", err)
	}

	// Append to existing few_shot_examples (merge, not replace)
	return qu.appendExamples(ctx, userID, result.FewShotExamples)
}

func (qu *QuickUpdater) appendExamples(ctx database.TenantContext, userID uuid.UUID, newExamples []FewShotPair) error {
	conn, err := qu.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	newJSON, _ := json.Marshal(newExamples)

	// Merge new examples with existing ones (append, cap at 20)
	_, err = conn.Exec(ctx,
		`INSERT INTO user_prompt_config (user_id, tenant_id, few_shot_examples, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (user_id) DO UPDATE SET
		   few_shot_examples = (
		     SELECT jsonb_agg(elem)
		     FROM (
		       SELECT elem FROM jsonb_array_elements(user_prompt_config.few_shot_examples) AS elem
		       UNION ALL
		       SELECT elem FROM jsonb_array_elements($3::jsonb) AS elem
		       LIMIT 20
		     ) sub
		   ),
		   updated_at = now()`,
		userID, ctx.TenantID, newJSON,
	)
	if err != nil {
		return fmt.Errorf("appending few-shot examples: %w", err)
	}
	return nil
}
