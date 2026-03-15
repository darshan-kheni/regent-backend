package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/models"
)

// PipelineStage represents a stage in the AI processing pipeline.
type PipelineStage string

const (
	StageQueued       PipelineStage = "queued"
	StageCategorizing PipelineStage = "categorizing"
	StageSummarizing  PipelineStage = "summarizing"
	StageDrafting     PipelineStage = "drafting"
	StageComplete     PipelineStage = "complete"
	StageError        PipelineStage = "error"
	StageSkipped      PipelineStage = "skipped"
)

// PlanStages defines which pipeline stages run for each plan.
var PlanStages = map[string][]PipelineStage{
	"free":    {StageCategorizing},                                  // categorize only
	"attache": {StageCategorizing, StageSummarizing, StageDrafting}, // cat+sum+standard
	"privy_council": {StageCategorizing, StageSummarizing, StageDrafting}, // full+auto-premium
	"estate":  {StageCategorizing, StageSummarizing, StageDrafting}, // full+premium-default
}

// draftTier maps plan names to the draft model tier passed to the DraftEngine.
var draftTier = map[string]string{
	"free":    "standard",
	"attache": "standard",
	"privy_council": "premium",
	"estate":  "premium",
}

// Categorizer abstracts email categorization + priority scoring.
// The returned interface{} is expected to be a *tasks.CategorizeResult (passed
// opaquely to the Drafter to avoid importing the tasks package and creating
// an import cycle: queue -> tasks -> ai -> queue).
type Categorizer interface {
	CategorizeAndPrioritize(ctx database.TenantContext, email models.Email, userID uuid.UUID) (interface{}, error)
}

// Summarizer abstracts email summarization.
type Summarizer interface {
	Summarize(ctx database.TenantContext, email models.Email, threadEmails []models.Email) (interface{}, error)
}

// Drafter abstracts draft reply generation. The catResult parameter is the
// opaque value returned by Categorizer.CategorizeAndPrioritize.
type Drafter interface {
	GenerateDraft(ctx database.TenantContext, email models.Email, tier string, catResult interface{}, userID uuid.UUID) (interface{}, error)
}

// Pipeline executes the 3-stage AI processing pipeline for a single email.
type Pipeline struct {
	pool        *pgxpool.Pool
	categorizer Categorizer
	summarizer  Summarizer
	drafter     Drafter
}

// NewPipeline creates a new Pipeline with all required dependencies.
func NewPipeline(
	pool *pgxpool.Pool,
	categorizer Categorizer,
	summarizer Summarizer,
	drafter Drafter,
) *Pipeline {
	return &Pipeline{
		pool:        pool,
		categorizer: categorizer,
		summarizer:  summarizer,
		drafter:     drafter,
	}
}

// Process implements the JobProcessor interface. It runs the AI pipeline stages
// appropriate for the job's plan on a single email.
func (p *Pipeline) Process(ctx context.Context, job *Job) error {
	tCtx := database.WithTenant(ctx, job.TenantID, job.UserID)

	// Load email from DB
	email, err := p.loadEmail(tCtx, job.EmailID)
	if err != nil {
		p.updateStatus(tCtx, job.EmailID, job.UserID, job.TenantID, job.Plan, StageError, err.Error(), "")
		return fmt.Errorf("loading email %s: %w", job.EmailID, err)
	}

	stages := PlanStages[job.Plan]
	if stages == nil {
		stages = PlanStages["free"]
	}

	start := time.Now()
	var catResult interface{}
	var sumResult interface{}

	// Stage 1: Categorize + Priority (single batched call)
	if ContainsStage(stages, StageCategorizing) {
		p.updateStatus(tCtx, job.EmailID, job.UserID, job.TenantID, job.Plan, StageCategorizing, "", "")

		catResult, err = p.categorizer.CategorizeAndPrioritize(tCtx, *email, job.UserID)
		if err != nil {
			slog.Error("pipeline: categorization failed",
				"email_id", job.EmailID,
				"error", err,
			)
			p.updateStatus(tCtx, job.EmailID, job.UserID, job.TenantID, job.Plan, StageError, err.Error(), "categorizing")
			return fmt.Errorf("categorizing: %w", err)
		}
	}

	// Brief pause between stages to avoid Ollama Cloud rate limits.
	time.Sleep(500 * time.Millisecond)

	// Stage 2: Summarize
	if ContainsStage(stages, StageSummarizing) {
		p.updateStatus(tCtx, job.EmailID, job.UserID, job.TenantID, job.Plan, StageSummarizing, "", "")

		// Pass nil for threadEmails — single email summarization
		sumResult, err = p.summarizer.Summarize(tCtx, *email, nil)
		if err != nil {
			slog.Error("pipeline: summarization failed",
				"email_id", job.EmailID,
				"error", err,
			)
			// Non-fatal: continue to next stage, but clear sumResult to avoid nil pointer
			sumResult = nil
			slog.Warn("pipeline: continuing despite summarization failure", "email_id", job.EmailID)
		}
	}

	time.Sleep(500 * time.Millisecond)

	// Stage 3: Draft Reply — only for emails that actually need a reply
	if ContainsStage(stages, StageDrafting) {
		skipReason := p.shouldSkipDraft(email, catResult, sumResult)
		if skipReason != "" {
			slog.Info("pipeline: skipping draft — email does not need a reply",
				"email_id", job.EmailID,
				"reason", skipReason,
			)
		} else {
			p.updateStatus(tCtx, job.EmailID, job.UserID, job.TenantID, job.Plan, StageDrafting, "", "")

			tier := draftTier[job.Plan]
			if tier == "" {
				tier = "standard"
			}

			_, err := p.drafter.GenerateDraft(tCtx, *email, tier, catResult, job.UserID)
			if err != nil {
				slog.Error("pipeline: draft generation failed",
					"email_id", job.EmailID,
					"error", err,
				)
				// Non-fatal
				slog.Warn("pipeline: continuing despite draft failure", "email_id", job.EmailID)
			}
		}
	}

	elapsed := time.Since(start)
	slog.Info("pipeline: email processed",
		"email_id", job.EmailID,
		"plan", job.Plan,
		"stages", len(stages),
		"duration_ms", elapsed.Milliseconds(),
	)

	p.updateStatus(tCtx, job.EmailID, job.UserID, job.TenantID, job.Plan, StageComplete, "", "")
	return nil
}

// loadEmail fetches an email by ID with RLS enforcement.
func (p *Pipeline) loadEmail(ctx database.TenantContext, emailID uuid.UUID) (*models.Email, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS: %w", err)
	}

	var email models.Email
	err = conn.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, account_id, subject, body_text, from_address, from_name,
				has_attachments, thread_id, received_at, COALESCE(direction, 'inbound')
		 FROM emails WHERE id = $1`, emailID,
	).Scan(
		&email.ID, &email.TenantID, &email.UserID, &email.AccountID,
		&email.Subject, &email.BodyText, &email.FromAddress, &email.FromName,
		&email.HasAttachments, &email.ThreadID, &email.ReceivedAt, &email.Direction,
	)
	if err != nil {
		return nil, fmt.Errorf("querying email: %w", err)
	}
	return &email, nil
}

// updateStatus writes to the email_ai_status table, inserting on first call
// and upserting on subsequent calls for the same email.
func (p *Pipeline) updateStatus(ctx database.TenantContext, emailID, userID, tenantID uuid.UUID, plan string, stage PipelineStage, errMsg, skippedReason string) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		slog.Error("pipeline: failed to acquire conn for status update", "error", err)
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		slog.Error("pipeline: failed to set RLS for status update", "error", err)
		return
	}

	var completedAt interface{}
	if stage == StageComplete || stage == StageError || stage == StageSkipped {
		now := time.Now()
		completedAt = &now
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO email_ai_status (email_id, user_id, tenant_id, plan, stage, started_at, completed_at, error_message, skipped_reason)
		 VALUES ($1, $2, $3, $4, $5, now(), $6, $7, $8)
		 ON CONFLICT (email_id) DO UPDATE SET
			stage = EXCLUDED.stage,
			completed_at = EXCLUDED.completed_at,
			retry_count = CASE WHEN EXCLUDED.stage = 'error' THEN email_ai_status.retry_count + 1 ELSE email_ai_status.retry_count END,
			error_message = EXCLUDED.error_message,
			skipped_reason = EXCLUDED.skipped_reason`,
		emailID, userID, tenantID, plan, string(stage), completedAt, errMsg, skippedReason,
	)
	if err != nil {
		slog.Error("pipeline: failed to update status",
			"email_id", emailID,
			"stage", stage,
			"error", err,
		)
	}
}

// noReplyPatterns matches sender addresses that should never get auto-drafted replies.
var noReplyPatterns = []string{
	"noreply@", "no-reply@", "donotreply@", "do-not-reply@", "do_not_reply@",
	"mailer-daemon@", "postmaster@", "notifications@", "notification@",
	"updates@", "news@", "newsletter@", "marketing@", "promo@", "promotions@",
	"info@", "support@", "billing@", "receipts@", "receipt@",
	"bounce@", "auto-confirm@", "autoconfirm@", "verify@",
	"alert@", "alerts@", "digest@", "feedback@", "hello@",
	"deals@", "offer@", "offers@", "shop@", "store@", "order@", "orders@",
	"campaign@", "announce@", "announcements@", "automated@", "system@",
	"unsubscribe@", "subscription@", "subscriptions@",
}

// skipCategories are email categories that typically don't need replies.
var skipCategories = map[string]bool{
	"newsletter":  true,
	"newsletters": true,
	"spam":        true,
	"promotions":  true,
	"promotion":   true,
	"marketing":   true,
	"updates":     true,
	"social":      true,
	"shopping":    true,
	"advertising": true,
	"shipping":    true,
	"events":      true,
	"subscriptions": true,
}

// shouldSkipDraft returns a non-empty reason if the email should NOT get an auto-draft.
func (p *Pipeline) shouldSkipDraft(email *models.Email, catResult, sumResult interface{}) string {
	sender := strings.ToLower(email.FromAddress)

	// 1. Skip noreply / automated senders
	for _, pattern := range noReplyPatterns {
		if strings.Contains(sender, pattern) {
			return "noreply sender: " + email.FromAddress
		}
	}

	// 2. Skip newsletter / spam / promo categories
	if catResult != nil {
		type catGetter interface{ GetCategory() string }
		if cg, ok := catResult.(catGetter); ok {
			cat := strings.ToLower(cg.GetCategory())
			if skipCategories[cat] {
				return "category: " + cat
			}
			// Also check without trailing 's' for singular/plural variants
			if len(cat) > 1 && cat[len(cat)-1] == 's' {
				if skipCategories[cat[:len(cat)-1]] {
					return "category: " + cat
				}
			}
		}
	}

	// 3. Skip emails where summarization says no action required
	if sumResult != nil {
		type actionGetter interface{ GetActionRequired() bool }
		if ag, ok := sumResult.(actionGetter); ok {
			if !ag.GetActionRequired() {
				return "no action required"
			}
		}
	}

	// 4. Skip outbound (sent) emails — we don't reply to ourselves
	if strings.EqualFold(email.Direction, "outbound") {
		return "outbound email"
	}

	return ""
}

// ContainsStage checks whether a target stage is present in the given slice.
func ContainsStage(stages []PipelineStage, target PipelineStage) bool {
	for _, s := range stages {
		if s == target {
			return true
		}
	}
	return false
}
