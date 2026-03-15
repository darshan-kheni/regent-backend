package behavior

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// ComputePeakHours generates a 24-element histogram of outbound emails by hour.
func (s *BehaviorService) ComputePeakHours(ctx database.TenantContext, userID uuid.UUID, dayStart, dayEnd time.Time, timezone string) ([24]int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return [24]int{}, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return [24]int{}, fmt.Errorf("setting RLS context: %w", err)
	}

	if timezone == "" {
		timezone = "UTC"
	}

	rows, err := conn.Query(ctx,
		`SELECT EXTRACT(HOUR FROM created_at AT TIME ZONE $4)::int as hour, COUNT(*)::int as cnt
		 FROM emails
		 WHERE user_id = $1
		   AND direction = 'outbound'
		   AND created_at >= $2 AND created_at < $3
		 GROUP BY hour
		 ORDER BY hour`,
		userID, dayStart, dayEnd, timezone,
	)
	if err != nil {
		return [24]int{}, fmt.Errorf("querying peak hours: %w", err)
	}
	defer rows.Close()

	var histogram [24]int
	for rows.Next() {
		var hour, cnt int
		if err := rows.Scan(&hour, &cnt); err != nil {
			return [24]int{}, fmt.Errorf("scanning peak hours row: %w", err)
		}
		if hour >= 0 && hour < 24 {
			histogram[hour] = cnt
		}
	}
	return histogram, nil
}
