package briefings

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DigestScheduler determines when to send digests based on user preferences.
type DigestScheduler struct {
	pool *pgxpool.Pool
}

// NewDigestScheduler creates a scheduler.
func NewDigestScheduler(pool *pgxpool.Pool) *DigestScheduler {
	return &DigestScheduler{pool: pool}
}

// DigestSchedule holds a user's digest delivery preferences.
type DigestSchedule struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	Frequency string // daily, twice_daily, weekly, off
	Hour      int    // Hour of day in user's timezone (0-23)
	Minute    int    // Minute of hour (0-59)
	Timezone  string
}

// GetUsersForDigest returns user IDs that should receive a digest now.
func (s *DigestScheduler) GetUsersForDigest(ctx context.Context) ([]DigestSchedule, error) {
	if s.pool == nil {
		return nil, nil
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("digest scheduler: acquire connection: %w", err)
	}
	defer conn.Release()

	// digest_time is a TIME column; extract hour and minute from it.
	rows, err := conn.Query(ctx,
		`SELECT p.user_id, u.tenant_id, p.digest_frequency,
		        EXTRACT(HOUR FROM p.digest_time)::int AS digest_hour,
		        EXTRACT(MINUTE FROM p.digest_time)::int AS digest_minute,
		        p.digest_timezone
		 FROM user_notification_prefs p
		 JOIN users u ON u.id = p.user_id
		 WHERE p.digest_enabled = true
		   AND p.digest_frequency != 'off'`,
	)
	if err != nil {
		return nil, fmt.Errorf("digest scheduler: query users: %w", err)
	}
	defer rows.Close()

	var schedules []DigestSchedule
	for rows.Next() {
		var ds DigestSchedule
		if err := rows.Scan(&ds.UserID, &ds.TenantID, &ds.Frequency, &ds.Hour, &ds.Minute, &ds.Timezone); err != nil {
			continue
		}
		if ds.Timezone == "" {
			ds.Timezone = "UTC"
		}

		if s.isDue(ds) {
			schedules = append(schedules, ds)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("digest scheduler: iterate rows: %w", err)
	}

	return schedules, nil
}

// isDue checks if a digest should be sent now based on the user's schedule.
func (s *DigestScheduler) isDue(ds DigestSchedule) bool {
	loc, err := time.LoadLocation(ds.Timezone)
	if err != nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	nowMinutes := now.Hour()*60 + now.Minute()
	schedMinutes := ds.Hour*60 + ds.Minute

	// Check within a 30-minute window around the scheduled time
	diff := nowMinutes - schedMinutes
	if diff < 0 {
		diff = -diff
	}

	switch ds.Frequency {
	case "daily":
		return diff <= 30
	case "twice_daily":
		// Also check 12 hours later
		diff2 := nowMinutes - (schedMinutes + 720)
		if diff2 < 0 {
			diff2 = -diff2
		}
		return diff <= 30 || diff2 <= 30
	case "weekly":
		return now.Weekday() == time.Monday && diff <= 30
	default:
		return false
	}
}

// GetLastDigestTime returns when the last digest was sent for a user.
func (s *DigestScheduler) GetLastDigestTime(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	if s.pool == nil {
		return time.Now().Add(-24 * time.Hour), nil
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("digest scheduler: acquire: %w", err)
	}
	defer conn.Release()

	var lastSent time.Time
	err = conn.QueryRow(ctx,
		`SELECT sent_at FROM digest_history WHERE user_id = $1 ORDER BY sent_at DESC LIMIT 1`,
		userID,
	).Scan(&lastSent)
	if err != nil {
		// No previous digest — default to 24h ago
		return time.Now().Add(-24 * time.Hour), nil
	}

	return lastSent, nil
}
