package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/api"
	"github.com/darshan-kheni/regent/internal/briefings"
	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/email/connection"
	"github.com/darshan-kheni/regent/internal/observability"
	"github.com/darshan-kheni/regent/internal/orchestrator"
)

func main() {
	// 1. Load config
	cfg := config.Load()

	// 2. Setup logging
	logger := observability.NewLogger(cfg.Environment)
	slog.SetDefault(logger)

	// 3. Check file descriptor limits (before creating connections)
	connection.CheckFDLimits()

	// 4. Connect database pool
	pool, err := database.NewPool(cfg.Database)
	if err != nil {
		slog.Error("failed to create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// 5. Run migrations (configurable — disabled in production by default)
	if cfg.RunMigrations {
		if err := database.RunMigrations(cfg.Database.URL); err != nil {
			slog.Error("failed to run migrations", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Info("skipping auto-migration (RUN_MIGRATIONS=false)")
	}

	// 5.5 Create Redis client (optional — AI features degrade gracefully without it)
	var rdb *redis.Client
	if cfg.Redis.URL != "" {
		opt, err := redis.ParseURL(cfg.Redis.URL)
		if err != nil {
			slog.Error("failed to parse Redis URL", "error", err)
			os.Exit(1)
		}
		opt.PoolSize = cfg.Redis.PoolSize
		rdb = redis.NewClient(opt)

		// Verify Redis connection
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			slog.Warn("Redis not available, AI features will be limited", "error", err)
			rdb = nil
		} else {
			slog.Info("Redis connected", "pool_size", cfg.Redis.PoolSize)
			defer rdb.Close()
		}
	}
	// 6. Create orchestrator
	orchCfg := &orchestrator.OrchestratorConfig{
		BootDelay:           cfg.Orchestrator.BootDelay,
		StaggerDuration:     cfg.Orchestrator.StaggerDuration,
		MaxConcurrentLogins: cfg.Orchestrator.MaxConcurrentLogins,
		HeartbeatInterval:   cfg.Orchestrator.HealthInterval,
		DrainTimeout:        cfg.Orchestrator.DrainTimeout,
	}
	registry := orchestrator.NewServiceRegistry(pool, orchCfg)

	// Wire encryption for IMAP credential decryption in email sync
	if cfg.Auth.EncryptionMasterKey != "" {
		rotEnc, encErr := crypto.NewRotatingEncryptor(cfg.Auth.EncryptionMasterKey, "")
		if encErr != nil {
			slog.Error("failed to create rotating encryptor for orchestrator", "error", encErr)
		} else {
			registry.SetEncryptor(rotEnc)
			slog.Info("orchestrator: IMAP encryption configured")
		}
	}

	// 6.5 Create and start briefing dispatcher (per-server, not per-user)
	if rdb != nil {
		channels := make(map[string]briefings.Channel)
		// Channels will be registered as they're configured
		notifRouter := briefings.NewNotificationRouter(channels, pool)
		rulesEngine := briefings.NewPriorityRulesEngine(pool)
		rateLimiter := briefings.NewRateLimiter(rdb)
		deliveryTracker := briefings.NewDeliveryTracker(pool)
		dispatcher := briefings.NewBriefingDispatcher(rdb, pool, notifRouter, rulesEngine, rateLimiter, deliveryTracker)

		go func() {
			if err := dispatcher.Run(context.Background()); err != nil && err != context.Canceled {
				slog.Error("briefing dispatcher failed", "error", err)
			}
		}()
	}

	// 6.7 Create AI provider
	var aiProvider ai.AIProvider
	if cfg.AI.OllamaAPIKey != "" {
		aiProvider = ai.NewOllamaCloudProvider(cfg.AI.OllamaCloudURL, cfg.AI.OllamaAPIKey)
		slog.Info("AI provider initialized", "url", cfg.AI.OllamaCloudURL)
	} else {
		slog.Warn("No OLLAMA_CLOUD_API_KEY set — AI features will return stub responses")
	}

	// Wire AI pipeline into orchestrator (before Boot so bundles get AI deps)
	if aiProvider != nil && rdb != nil {
		registry.SetAIProvider(aiProvider, rdb)
		slog.Info("orchestrator: AI pipeline wired")
	}

	// Boot orchestrator in background (staggered user bundle spawning)
	go func() {
		if err := registry.Boot(context.Background()); err != nil {
			slog.Error("orchestrator boot failed", "error", err)
		}
	}()

	// 7. Build router
	router, err := api.NewRouter(cfg, pool, registry, rdb, aiProvider)
	if err != nil {
		slog.Error("failed to create router", "error", err)
		os.Exit(1)
	}

	// 8. Start server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 9. Graceful shutdown
	go func() {
		slog.Info("server starting", "port", cfg.Port, "env", cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("server shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced shutdown", "error", err)
	}

	// Drain all user service bundles
	slog.Info("draining orchestrator...")
	if err := registry.DrainAll(cfg.Orchestrator.DrainTimeout); err != nil {
		slog.Error("orchestrator drain error", "error", err)
	}

	slog.Info("server stopped")
}
