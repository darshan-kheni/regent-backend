package behavior

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// computeWeekendBoundary counts emails sent on Saturday/Sunday of the current week.
// ok < 3, warn 3-10, critical > 10
func (s *BehaviorService) computeWeekendBoundary(ctx database.TenantContext, userID uuid.UUID, date time.Time) StressIndicator {
	ind := StressIndicator{Metric: "weekend_boundary", Status: "ok"}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		ind.Detail = "failed to acquire connection"
		return ind
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		ind.Detail = "failed to set RLS context"
		return ind
	}

	// Find the most recent Saturday (start of weekend)
	daysUntilSat := int(date.Weekday()) - int(time.Saturday)
	if daysUntilSat < 0 {
		daysUntilSat += 7
	}
	weekendStart := date.AddDate(0, 0, -daysUntilSat)
	weekendEnd := weekendStart.AddDate(0, 0, 2) // Saturday + Sunday

	var count int
	conn.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM emails
		 WHERE user_id = $1
		   AND direction = 'outbound'
		   AND EXTRACT(DOW FROM created_at) IN (0, 6)
		   AND created_at >= $2 AND created_at < $3`,
		userID,
		weekendStart.Format("2006-01-02"),
		weekendEnd.Format("2006-01-02"),
	).Scan(&count)

	ind.Value = fmt.Sprintf("%d emails", count)

	switch {
	case count > 10:
		ind.Status = "critical"
		ind.Detail = fmt.Sprintf("Heavy weekend activity: %d emails sent", count)
	case count >= 3:
		ind.Status = "warn"
		ind.Detail = fmt.Sprintf("Some weekend activity: %d emails sent", count)
	default:
		ind.Status = "ok"
		ind.Detail = "Weekend boundary respected"
	}

	return ind
}
