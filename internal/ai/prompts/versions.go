package prompts

import (
	"context"
	"hash/fnv"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/ai"
)

// PromptVersioner manages versioned prompt templates with A/B testing.
type PromptVersioner struct {
	pool *pgxpool.Pool
}

// NewPromptVersioner creates a PromptVersioner.
func NewPromptVersioner(pool *pgxpool.Pool) *PromptVersioner {
	return &PromptVersioner{pool: pool}
}

// GetActiveVersion retrieves the appropriate active prompt template for a user,
// using consistent hashing for A/B split (50/50 by user_id hash).
func (pv *PromptVersioner) GetActiveVersion(ctx context.Context, taskType ai.TaskType, userID uuid.UUID) (string, int, error) {
	if pv.pool == nil {
		return "", 0, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := pv.pool.Query(ctx,
		`SELECT version, template FROM prompt_versions
		 WHERE task_type = $1 AND is_active = true
		 ORDER BY version`,
		string(taskType),
	)
	if err != nil {
		slog.Warn("failed to query prompt versions", "task_type", taskType, "error", err)
		return "", 0, nil
	}
	defer rows.Close()

	type versionEntry struct {
		version  int
		template string
	}
	var versions []versionEntry
	for rows.Next() {
		var v versionEntry
		if err := rows.Scan(&v.version, &v.template); err != nil {
			return "", 0, err
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}

	if len(versions) == 0 {
		return "", 0, nil
	}

	if len(versions) == 1 {
		return versions[0].template, versions[0].version, nil
	}

	// A/B split: consistent per user via FNV hash of user_id
	idx := userHash(userID) % uint64(len(versions))
	selected := versions[idx]

	slog.Debug("prompt A/B selection",
		"task_type", taskType,
		"user_id", userID,
		"version", selected.version,
		"total_versions", len(versions),
	)

	return selected.template, selected.version, nil
}

// userHash produces a consistent hash of a UUID for A/B splitting.
func userHash(id uuid.UUID) uint64 {
	h := fnv.New64a()
	h.Write(id[:])
	return h.Sum64()
}
