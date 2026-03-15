package behavior

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// ComputeToneDistribution aggregates tone classifications from emails for a date range.
// Returns percentages by tone type: {"professional": 45.0, "warm_friendly": 25.0, ...}
func (s *BehaviorService) ComputeToneDistribution(ctx database.TenantContext, userID uuid.UUID, dayStart, dayEnd time.Time) (map[string]float64, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT tone_classification, COUNT(*) as cnt
		 FROM emails
		 WHERE user_id = $1
		   AND tone_classification IS NOT NULL
		   AND created_at >= $2 AND created_at < $3
		 GROUP BY tone_classification`,
		userID, dayStart, dayEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("querying tone distribution: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int64)
	var total int64
	for rows.Next() {
		var tone string
		var cnt int64
		if err := rows.Scan(&tone, &cnt); err != nil {
			return nil, fmt.Errorf("scanning tone row: %w", err)
		}
		counts[tone] = cnt
		total += cnt
	}

	dist := make(map[string]float64, len(counts))
	if total > 0 {
		for tone, cnt := range counts {
			dist[tone] = float64(cnt) / float64(total) * 100.0
		}
	}
	return dist, nil
}
