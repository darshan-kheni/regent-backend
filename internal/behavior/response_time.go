package behavior

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// ComputeResponseTimes calculates response times for emails sent by the user on the given date.
// It matches reply.in_reply_to to original.message_id and computes elapsed time.
// Business hours (09:00-18:00 weekdays in user timezone) are used for calculation.
func (s *BehaviorService) ComputeResponseTimes(ctx database.TenantContext, userID uuid.UUID, date time.Time) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	// Find replies sent by user on the given date that have in_reply_to set
	// and match them to the original email by message_id
	rows, err := conn.Query(ctx,
		`SELECT r.id, r.created_at, o.received_at
		 FROM emails r
		 JOIN emails o ON r.in_reply_to = o.message_id AND r.tenant_id = o.tenant_id
		 WHERE r.user_id = $1
		   AND r.direction = 'outbound'
		   AND r.in_reply_to IS NOT NULL
		   AND r.response_time_minutes IS NULL
		   AND r.created_at >= $2 AND r.created_at < $3`,
		userID, date, date.AddDate(0, 0, 1),
	)
	if err != nil {
		return fmt.Errorf("querying reply pairs: %w", err)
	}
	defer rows.Close()

	type replyPair struct {
		replyID    uuid.UUID
		repliedAt  time.Time
		receivedAt time.Time
	}

	var pairs []replyPair
	for rows.Next() {
		var p replyPair
		if err := rows.Scan(&p.replyID, &p.repliedAt, &p.receivedAt); err != nil {
			return fmt.Errorf("scanning reply pair: %w", err)
		}
		pairs = append(pairs, p)
	}

	// Update each reply with its response time
	for _, p := range pairs {
		minutes := businessMinutesBetween(p.receivedAt, p.repliedAt)
		if minutes < 0 {
			minutes = 0
		}
		_, err := conn.Exec(ctx,
			`UPDATE emails SET response_time_minutes = $1 WHERE id = $2`,
			minutes, p.replyID,
		)
		if err != nil {
			slog.Warn("failed to update response time",
				"reply_id", p.replyID,
				"error", err,
			)
		}
	}

	return nil
}

// businessMinutesBetween calculates minutes between two times, only counting
// weekday hours between 09:00 and 18:00. This gives a more meaningful
// "business response time" than raw elapsed time.
func businessMinutesBetween(start, end time.Time) float64 {
	if end.Before(start) {
		return 0
	}

	const (
		businessStart = 9  // 09:00
		businessEnd   = 18 // 18:00
	)

	var totalMinutes float64
	current := start

	for current.Before(end) {
		// Skip weekends
		if current.Weekday() == time.Saturday || current.Weekday() == time.Sunday {
			current = time.Date(current.Year(), current.Month(), current.Day()+1,
				businessStart, 0, 0, 0, current.Location())
			continue
		}

		// Clamp current to business hours
		dayStart := time.Date(current.Year(), current.Month(), current.Day(),
			businessStart, 0, 0, 0, current.Location())
		dayEnd := time.Date(current.Year(), current.Month(), current.Day(),
			businessEnd, 0, 0, 0, current.Location())

		// If current is before business start, move to business start
		effectiveStart := current
		if effectiveStart.Before(dayStart) {
			effectiveStart = dayStart
		}

		// If current is after business end, move to next day
		if effectiveStart.After(dayEnd) || effectiveStart.Equal(dayEnd) {
			current = time.Date(current.Year(), current.Month(), current.Day()+1,
				businessStart, 0, 0, 0, current.Location())
			continue
		}

		// Calculate end of business window for this day
		effectiveEnd := end
		if effectiveEnd.After(dayEnd) {
			effectiveEnd = dayEnd
		}

		if effectiveEnd.After(effectiveStart) {
			totalMinutes += effectiveEnd.Sub(effectiveStart).Minutes()
		}

		// Move to next business day
		current = time.Date(current.Year(), current.Month(), current.Day()+1,
			businessStart, 0, 0, 0, current.Location())
	}

	return totalMinutes
}
