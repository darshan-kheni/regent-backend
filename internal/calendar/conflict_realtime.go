package calendar

import (
	"log/slog"
	"time"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConflictChecker runs after each calendar sync to detect and store conflicts.
type ConflictChecker struct {
	pool *pgxpool.Pool
}

// NewConflictChecker creates a ConflictChecker backed by the given connection pool.
func NewConflictChecker(pool *pgxpool.Pool) *ConflictChecker {
	return &ConflictChecker{pool: pool}
}

// CheckAfterSync detects conflicts for events in the given time range after a sync.
func (cc *ConflictChecker) CheckAfterSync(ctx database.TenantContext, start, end time.Time) error {
	events, err := cc.getEventsInRange(ctx, start, end)
	if err != nil {
		return err
	}

	prefs, err := cc.getPreferences(ctx)
	if err != nil {
		return err
	}

	conflicts := DetectConflicts(events, prefs)

	// Clear old unresolved conflicts for this time range, then insert new ones
	if err := cc.replaceConflicts(ctx, start, end, conflicts); err != nil {
		return err
	}

	slog.Debug("conflict check complete", "user_id", ctx.UserID, "events", len(events), "conflicts", len(conflicts))
	return nil
}

func (cc *ConflictChecker) getEventsInRange(ctx database.TenantContext, start, end time.Time) ([]CalendarEvent, error) {
	conn, err := cc.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, account_id, provider, provider_event_id, calendar_id,
			title, description, start_time, end_time, time_zone, location,
			is_all_day, status, attendees, recurrence_rule, organizer_email,
			is_online, meeting_url, briefed_at, last_synced, created_at, updated_at
		FROM calendar_events
		WHERE user_id = $1 AND start_time < $3 AND end_time > $2 AND status != 'cancelled'
		ORDER BY start_time`, ctx.UserID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.TenantID, &e.AccountID, &e.Provider, &e.ProviderEventID, &e.CalendarID,
			&e.Title, &e.Description, &e.StartTime, &e.EndTime, &e.TimeZone, &e.Location,
			&e.IsAllDay, &e.Status, &e.Attendees, &e.RecurrenceRule, &e.OrganizerEmail,
			&e.IsOnline, &e.MeetingURL, &e.BriefedAt, &e.LastSynced, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			slog.Error("conflict checker: scan error", "err", err)
			continue
		}
		events = append(events, e)
	}
	return events, nil
}

func (cc *ConflictChecker) getPreferences(ctx database.TenantContext) (*CalendarPreference, error) {
	conn, err := cc.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, err
	}

	var p CalendarPreference
	err = conn.QueryRow(ctx,
		`SELECT id, user_id, tenant_id, preferred_start_hour, preferred_end_hour, buffer_minutes,
			no_meeting_days, focus_blocks, home_timezone
		FROM calendar_preferences WHERE user_id = $1`, ctx.UserID,
	).Scan(&p.ID, &p.UserID, &p.TenantID, &p.PreferredStartHour, &p.PreferredEndHour,
		&p.BufferMinutes, &p.NoMeetingDays, &p.FocusBlocks, &p.HomeTimezone)
	if err != nil {
		// Return defaults
		return &CalendarPreference{
			PreferredStartHour: 9,
			PreferredEndHour:   18,
			BufferMinutes:      15,
			NoMeetingDays:      []byte("[]"),
			FocusBlocks:        []byte("[]"),
			HomeTimezone:       "UTC",
		}, nil
	}
	return &p, nil
}

func (cc *ConflictChecker) replaceConflicts(ctx database.TenantContext, start, end time.Time, conflicts []Conflict) error {
	conn, err := cc.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete old unresolved conflicts for events in this range
	_, err = tx.Exec(ctx,
		`DELETE FROM calendar_conflicts
		WHERE user_id = $1 AND NOT resolved
		AND event_a_id IN (
			SELECT id FROM calendar_events WHERE user_id = $1 AND start_time < $3 AND end_time > $2
		)`, ctx.UserID, start, end)
	if err != nil {
		return err
	}

	// Insert new conflicts
	for _, c := range conflicts {
		_, err = tx.Exec(ctx,
			`INSERT INTO calendar_conflicts (user_id, tenant_id, event_a_id, event_b_id, type, severity, overlap_min, gap_min, detail)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (event_a_id, event_b_id) DO UPDATE SET
				type = EXCLUDED.type, severity = EXCLUDED.severity,
				overlap_min = EXCLUDED.overlap_min, gap_min = EXCLUDED.gap_min,
				detail = EXCLUDED.detail, resolved = false`,
			c.UserID, c.TenantID, c.EventAID, c.EventBID, c.Type, c.Severity, c.OverlapMin, c.GapMin, c.Detail)
		if err != nil {
			slog.Error("insert conflict failed", "err", err)
		}
	}

	return tx.Commit(ctx)
}
