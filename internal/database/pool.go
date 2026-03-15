package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go/pgx"

	"github.com/darshan-kheni/regent/internal/config"
)

// NewPool creates a production-ready pgxpool with health checks, lifecycle hooks,
// and graceful shutdown support.
func NewPool(cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}

	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = 1 * time.Minute
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second

	poolConfig.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		if err := conn.Ping(ctx); err != nil {
			slog.Warn("rejecting unhealthy connection", "error", err)
			return false
		}
		return true
	}

	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgvector.RegisterTypes(ctx, conn); err != nil {
			return fmt.Errorf("registering pgvector types: %w", err)
		}
		slog.Debug("new database connection established")
		return nil
	}

	poolConfig.AfterRelease = func(conn *pgx.Conn) bool {
		// Reset tenant context when connection returns to pool to prevent
		// cross-tenant data leakage between pool users.
		_, _ = conn.Exec(context.Background(), "RESET ALL")
		return true
	}

	poolConfig.BeforeClose = func(conn *pgx.Conn) {
		slog.Debug("database connection closing")
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	slog.Info("database pool connected",
		"max_conns", cfg.MaxConns,
		"min_conns", cfg.MinConns,
	)
	return pool, nil
}
