package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/memory"
	"github.com/darshan-kheni/regent/internal/ai/pipeline"
	"github.com/darshan-kheni/regent/internal/ai/prompts"
	"github.com/darshan-kheni/regent/internal/ai/queue"
	"github.com/darshan-kheni/regent/internal/ai/rag"
	"github.com/darshan-kheni/regent/internal/ai/tasks"
	"github.com/darshan-kheni/regent/internal/crypto"
)

// ServiceRegistry maps user_id -> *UserServiceBundle and manages the lifecycle
// of all user service bundles. It is the top-level orchestrator component.
type ServiceRegistry struct {
	mu        sync.RWMutex
	bundles   map[uuid.UUID]*UserServiceBundle
	pool      *pgxpool.Pool
	cfg       *OrchestratorConfig
	encryptor *crypto.RotatingEncryptor
	wg        sync.WaitGroup

	// AI pipeline dependencies (set via SetAIProvider before Boot).
	rdb        *redis.Client
	aiProvider ai.AIProvider
	aiQueue    *queue.Queue
	workerPool *queue.WorkerPool
}

// NewServiceRegistry creates a new registry backed by the given connection pool.
func NewServiceRegistry(pool *pgxpool.Pool, cfg *OrchestratorConfig) *ServiceRegistry {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &ServiceRegistry{
		bundles: make(map[uuid.UUID]*UserServiceBundle),
		pool:    pool,
		cfg:     cfg,
	}
}

// Boot queries all active users and spawns service bundles for each, staggered
// over cfg.StaggerDuration to avoid thundering herd on IMAP connections.
// It waits cfg.BootDelay before starting to allow the server to fully initialize.
func (r *ServiceRegistry) Boot(ctx context.Context) error {
	slog.Info("registry: waiting for boot delay",
		"delay", r.cfg.BootDelay,
	)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(r.cfg.BootDelay):
	}

	slog.Info("registry: querying active users")

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection for boot: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT id, tenant_id FROM users WHERE status = 'active'`,
	)
	if err != nil {
		return fmt.Errorf("querying active users: %w", err)
	}
	defer rows.Close()

	type userInfo struct {
		userID   uuid.UUID
		tenantID uuid.UUID
	}
	var users []userInfo

	for rows.Next() {
		var u userInfo
		if err := rows.Scan(&u.userID, &u.tenantID); err != nil {
			slog.Error("registry: failed to scan user row", "error", err)
			continue
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating user rows: %w", err)
	}

	if len(users) == 0 {
		slog.Info("registry: no active users found")
		return nil
	}

	slog.Info("registry: spawning user bundles",
		"user_count", len(users),
		"stagger_duration", r.cfg.StaggerDuration,
	)

	// Calculate delay between spawns to stagger over the configured duration.
	var staggerDelay time.Duration
	if len(users) > 1 {
		staggerDelay = r.cfg.StaggerDuration / time.Duration(len(users)-1)
	}

	// Semaphore to limit concurrent logins.
	sem := make(chan struct{}, r.cfg.MaxConcurrentLogins)

	for i, u := range users {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		sem <- struct{}{}

		r.Spawn(ctx, u.userID, u.tenantID)

		<-sem

		// Stagger delay between spawns (skip after last user).
		if i < len(users)-1 && staggerDelay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(staggerDelay):
			}
		}
	}

	// Start the global AI worker pool (pulls jobs from Redis queue).
	if r.workerPool != nil {
		go func() {
			if err := r.workerPool.Run(ctx); err != nil {
				slog.Error("registry: AI worker pool error", "error", err)
			}
		}()
		slog.Info("registry: AI worker pool started")
	}

	slog.Info("registry: boot complete",
		"bundles_spawned", r.Count(),
	)
	return nil
}

// Spawn creates and starts a service bundle for a user. Idempotent — if a bundle
// already exists for the user, this is a no-op.
func (r *ServiceRegistry) Spawn(ctx context.Context, userID, tenantID uuid.UUID) {
	r.mu.Lock()
	if _, exists := r.bundles[userID]; exists {
		r.mu.Unlock()
		return
	}

	bundleCtx, cancel := context.WithCancel(ctx)
	bundle := NewUserServiceBundle(userID, tenantID, bundleCtx, cancel, r.pool, r.cfg)
	if r.encryptor != nil {
		bundle.SetEncryptor(r.encryptor)
	}
	bundle.SetAIDeps(r.aiProvider, r.rdb)
	r.bundles[userID] = bundle
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.bundles, userID)
			r.mu.Unlock()
		}()
		bundle.Run()
	}()

	slog.Info("registry: spawned user bundle",
		"user_id", userID,
		"tenant_id", tenantID,
	)
}

// SetEncryptor sets the rotating encryptor for IMAP credential decryption on all bundles.
func (r *ServiceRegistry) SetEncryptor(enc *crypto.RotatingEncryptor) {
	r.encryptor = enc
}

// SetAIProvider configures the AI pipeline dependencies. Must be called before Boot().
// Creates the Redis-backed queue and constructs the global WorkerPool with the full
// categorize -> summarize -> draft pipeline.
func (r *ServiceRegistry) SetAIProvider(provider ai.AIProvider, rdb *redis.Client) {
	r.aiProvider = provider
	r.rdb = rdb

	if provider == nil || rdb == nil {
		return
	}

	// Build the AI pipeline components.
	router := ai.NewModelRouter()
	embedder := rag.NewEmbedder(provider)
	vectorStore := rag.NewVectorStore(r.pool)
	retriever := rag.NewRetriever(vectorStore, embedder)
	builder := prompts.NewPromptBuilder(r.pool)
	ruleStore := memory.NewUserRuleStore(r.pool)
	briefStore := memory.NewContextBriefStore(r.pool)
	patternStore := memory.NewLearnedPatternStore(r.pool)
	injector := memory.NewContextInjector(ruleStore, briefStore, patternStore)

	// Create task engines.
	catEngine := tasks.NewCategorizeEngine(r.pool, provider, router, retriever, builder, injector, ruleStore, rdb)
	sumEngine := tasks.NewSummarizeEngine(r.pool, provider, router, retriever, builder, injector, rdb)
	draftEngine := tasks.NewDraftEngine(r.pool, provider, router, retriever, builder, injector)

	// Wrap in adapters to satisfy queue.Pipeline interfaces.
	categorizer := &pipeline.CategorizeAdapter{Engine: catEngine}
	summarizer := &pipeline.SummarizeAdapter{Engine: sumEngine}
	drafter := &pipeline.DraftAdapter{Engine: draftEngine}

	// Create the pipeline (implements queue.JobProcessor).
	pipe := queue.NewPipeline(r.pool, categorizer, summarizer, drafter)

	// Create Redis-backed queue and global worker pool (1 concurrent worker).
	// Limited to 1 to avoid Ollama Cloud rate limits (429 errors).
	r.aiQueue = queue.NewQueue(rdb)
	r.workerPool = queue.NewWorkerPool(r.aiQueue, pipe, 1)

	slog.Info("registry: AI pipeline configured",
		"worker_capacity", 1,
	)
}

// Stop cancels the service bundle for a specific user.
func (r *ServiceRegistry) Stop(userID uuid.UUID) {
	r.mu.RLock()
	bundle, exists := r.bundles[userID]
	r.mu.RUnlock()

	if !exists {
		return
	}

	slog.Info("registry: stopping user bundle", "user_id", userID)
	bundle.Stop()
}

// DrainAll cancels all bundles and waits for them to stop, with a timeout.
// Returns an error if the timeout is exceeded.
func (r *ServiceRegistry) DrainAll(timeout time.Duration) error {
	slog.Info("registry: draining all bundles",
		"timeout", timeout,
		"bundle_count", r.Count(),
	)

	r.mu.RLock()
	for _, bundle := range r.bundles {
		bundle.Stop()
	}
	r.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("registry: all bundles drained successfully")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("drain timed out after %v, some bundles may still be running", timeout)
	}
}

// Count returns the number of currently active bundles.
func (r *ServiceRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.bundles)
}

// IsRunning checks if a specific user has an active service bundle.
func (r *ServiceRegistry) IsRunning(userID uuid.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.bundles[userID]
	return exists
}
