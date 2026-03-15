package ai

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BenchmarkResult stores the outcome of a model performance test.
type BenchmarkResult struct {
	ID           uuid.UUID
	TaskType     string
	Model        string
	Accuracy     float64
	LatencyP50   int
	LatencyP99   int
	TokensPerSec int
	TestedAt     time.Time
}

// BenchmarkStore persists benchmark results.
type BenchmarkStore struct {
	pool *pgxpool.Pool
}

// NewBenchmarkStore creates a new BenchmarkStore.
func NewBenchmarkStore(pool *pgxpool.Pool) *BenchmarkStore {
	return &BenchmarkStore{pool: pool}
}

// Store saves a benchmark result.
func (s *BenchmarkStore) Store(ctx context.Context, result BenchmarkResult) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO benchmark_results (task_type, model, accuracy, latency_p50, latency_p99, tokens_per_sec)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		result.TaskType, result.Model, result.Accuracy, result.LatencyP50, result.LatencyP99, result.TokensPerSec,
	)
	return err
}

// RunBenchmarkHarness is a stub for the monthly model re-evaluation harness.
// TODO: Implement with 200 labeled emails post-launch.
func RunBenchmarkHarness(_ context.Context, _ *pgxpool.Pool, _ AIProvider) error {
	return nil
}
