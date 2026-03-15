package orchestrator

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/crypto"
)

// UserServiceBundle represents the set of always-on services for a single user.
// Each bundle runs independently with its own context and cancel function.
// On Run(), it launches a health reporter, IMAP watcher, cron scheduler,
// and AI processor as supervised goroutines, then blocks until its context is cancelled.
type UserServiceBundle struct {
	UserID   uuid.UUID
	TenantID uuid.UUID

	ctx    context.Context
	cancel context.CancelFunc
	pool   *pgxpool.Pool
	cfg    *OrchestratorConfig
	health *HealthReporter

	// encryptor for decrypting IMAP credentials during email sync.
	encryptor *crypto.RotatingEncryptor

	// AI pipeline dependencies for the cron scheduler's aiQueueProcess job.
	aiProvider ai.AIProvider
	rdb        *redis.Client

	// aiRunner is an optional function that runs the AI processor goroutine.
	// Set via SetAIRunner before calling Run().
	aiRunner func(context.Context) error

	// briefingRunner is an optional function that runs the briefing engine goroutine.
	// Set via SetBriefingRunner before calling Run().
	briefingRunner func(context.Context) error
}

// NewUserServiceBundle creates a new bundle for the given user.
// The caller must provide a cancellable context (typically derived from the registry's root context).
func NewUserServiceBundle(
	userID, tenantID uuid.UUID,
	ctx context.Context,
	cancel context.CancelFunc,
	pool *pgxpool.Pool,
	cfg *OrchestratorConfig,
) *UserServiceBundle {
	return &UserServiceBundle{
		UserID:   userID,
		TenantID: tenantID,
		ctx:      ctx,
		cancel:   cancel,
		pool:     pool,
		cfg:      cfg,
		health:   NewHealthReporter(userID, tenantID, pool, cfg.HeartbeatInterval),
	}
}

// Run starts all sub-services with supervision and blocks until the bundle's
// context is cancelled. Internal goroutines are managed here — they are not
// exported or externally visible.
func (b *UserServiceBundle) Run() {
	slog.Info("bundle: starting user services",
		"user_id", b.UserID,
		"tenant_id", b.TenantID,
	)

	var wg sync.WaitGroup

	// Health reporter runs without supervision (it's the monitoring itself).
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.health.Run(b.ctx)
	}()

	// IMAP watcher with supervisor restart.
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.runWithSupervisor("imap", b.runIMAPWatcher)
	}()

	// Cron scheduler with supervisor restart.
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.runWithSupervisor("cron", b.runCronScheduler)
	}()

	// AI processor with supervisor restart (if configured).
	if b.aiRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.runWithSupervisor("ai", b.runAIProcessor)
		}()
	}

	// Briefing engine with supervisor restart (if configured).
	if b.briefingRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.runWithSupervisor("briefing", b.runBriefingEngine)
		}()
	}

	// Block until context is done, then wait for all goroutines to exit.
	<-b.ctx.Done()
	wg.Wait()

	slog.Info("bundle: all services stopped",
		"user_id", b.UserID,
	)
}

// Stop cancels the bundle's context, causing Run() to unblock and all
// supervised services to wind down.
func (b *UserServiceBundle) Stop() {
	b.cancel()
}

// runIMAPWatcher is a placeholder for the IMAP real-time email watcher.
// Actual IMAP integration will be wired in a later task.
func (b *UserServiceBundle) runIMAPWatcher(ctx context.Context) error {
	b.health.SetStatus("imap", "running")
	<-ctx.Done()
	return nil
}

// runCronScheduler creates and runs the per-user cron scheduler.
func (b *UserServiceBundle) runCronScheduler(ctx context.Context) error {
	scheduler := NewCronScheduler(b.UserID, b.TenantID, b.pool, b.encryptor, b.aiProvider, b.rdb)
	return scheduler.Run(ctx)
}

// SetEncryptor sets the rotating encryptor for IMAP credential decryption.
// Must be called before Run().
func (b *UserServiceBundle) SetEncryptor(enc *crypto.RotatingEncryptor) {
	b.encryptor = enc
}

// SetAIDeps sets the AI provider and Redis client for use by the cron scheduler's
// AI queue processing job. Must be called before Run().
func (b *UserServiceBundle) SetAIDeps(provider ai.AIProvider, rdb *redis.Client) {
	b.aiProvider = provider
	b.rdb = rdb
}

// SetAIRunner sets the AI processor function to be run as a supervised goroutine.
// Must be called before Run().
func (b *UserServiceBundle) SetAIRunner(fn func(context.Context) error) {
	b.aiRunner = fn
}

// runAIProcessor delegates to the configured AI runner.
func (b *UserServiceBundle) runAIProcessor(ctx context.Context) error {
	b.health.SetStatus("ai", "running")
	err := b.aiRunner(ctx)
	if err != nil {
		b.health.SetStatus("ai", "error")
	}
	return err
}

// SetBriefingRunner sets the briefing engine function to be run as a supervised goroutine.
// Must be called before Run().
func (b *UserServiceBundle) SetBriefingRunner(fn func(context.Context) error) {
	b.briefingRunner = fn
}

// runBriefingEngine delegates to the configured briefing runner.
func (b *UserServiceBundle) runBriefingEngine(ctx context.Context) error {
	b.health.SetStatus("briefing", "running")
	err := b.briefingRunner(ctx)
	if err != nil {
		b.health.SetStatus("briefing", "error")
	}
	return err
}
