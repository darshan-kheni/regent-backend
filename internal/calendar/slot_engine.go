package calendar

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SlotEngine generates and scores meeting time slot suggestions.
type SlotEngine struct {
	pool *pgxpool.Pool
	cfg  *config.CalendarConfig
}

// NewSlotEngine creates a SlotEngine backed by the given connection pool and config.
func NewSlotEngine(pool *pgxpool.Pool, cfg *config.CalendarConfig) *SlotEngine {
	return &SlotEngine{pool: pool, cfg: cfg}
}

// SuggestSlots finds the top N available time slots for the given request.
func (se *SlotEngine) SuggestSlots(ctx database.TenantContext, req SlotRequest) ([]SuggestedSlot, error) {
	prefs, err := se.getPreferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading preferences: %w", err)
	}

	// Determine search range
	start := req.PreferredStart
	end := req.PreferredEnd
	if start.IsZero() {
		start = time.Now().Truncate(24 * time.Hour).Add(time.Duration(prefs.PreferredStartHour) * time.Hour)
	}
	if end.IsZero() {
		end = start.Add(7 * 24 * time.Hour)
	}

	// Get user's existing events
	events, err := se.getEventsInRange(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("loading events: %w", err)
	}

	duration := time.Duration(req.DurationMinutes) * time.Minute
	if duration == 0 {
		duration = 60 * time.Minute
	}
	increment := time.Duration(se.cfg.SlotIncrementMinutes) * time.Minute

	// Generate candidates at configured increments
	var candidates []TimeSlot
	for t := start; t.Add(duration).Before(end) || t.Add(duration).Equal(end); t = t.Add(increment) {
		slot := TimeSlot{Start: t, End: t.Add(duration)}

		// Skip no-meeting days
		if isNoMeetingDay(slot.Start, prefs) {
			continue
		}

		// Only consider business hours
		hour := slot.Start.Hour()
		endHour := slot.End.Hour()
		if slot.End.Minute() > 0 {
			endHour++
		}
		if hour < prefs.PreferredStartHour || endHour > prefs.PreferredEndHour {
			continue
		}

		// Check for conflicts with existing events
		if hasConflict(slot, events, prefs) {
			continue
		}

		candidates = append(candidates, slot)
	}

	// Score and rank candidates
	input := ScoreInput{
		UserPrefs:     prefs,
		Events:        events,
		MeetingType:   req.MeetingType,
		ProposedTimes: req.ProposedTimes(),
		Attendees:     req.Attendees,
	}

	scored := make([]ScoredSlot, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, ScoreSlot(c, input))
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Return top N
	maxSlots := se.cfg.MaxSlotSuggestions
	if len(scored) < maxSlots {
		maxSlots = len(scored)
	}

	suggestions := make([]SuggestedSlot, 0, maxSlots)
	for i := 0; i < maxSlots; i++ {
		s := scored[i]
		suggestions = append(suggestions, SuggestedSlot{
			Start:     s.Slot.Start,
			End:       s.Slot.End,
			Score:     s.Score,
			Reasoning: s.Reasoning(),
		})
	}

	return suggestions, nil
}

// TimeSlot represents a candidate time slot.
type TimeSlot struct {
	Start time.Time
	End   time.Time
}

// hasConflict checks if a candidate slot overlaps with any existing event.
func hasConflict(slot TimeSlot, events []CalendarEvent, prefs *CalendarPreference) bool {
	buffer := time.Duration(prefs.BufferMinutes) * time.Minute
	for _, e := range events {
		if e.Status == "cancelled" || e.IsAllDay {
			continue
		}
		// Check overlap including buffer
		if slot.Start.Before(e.EndTime.Add(buffer)) && slot.End.After(e.StartTime.Add(-buffer)) {
			return true
		}
	}
	return false
}

func isNoMeetingDay(t time.Time, prefs *CalendarPreference) bool {
	var days []int
	if prefs.NoMeetingDays != nil {
		_ = json.Unmarshal(prefs.NoMeetingDays, &days)
	}
	weekday := int(t.Weekday())
	for _, d := range days {
		if weekday == d {
			return true
		}
	}
	return false
}

// ProposedTimes returns an empty slice; proposed times are populated from SchedulingAnalysis.
func (sr SlotRequest) ProposedTimes() []time.Time {
	return nil
}

// getPreferences loads user preferences with defaults.
func (se *SlotEngine) getPreferences(ctx database.TenantContext) (*CalendarPreference, error) {
	cc := &ConflictChecker{pool: se.pool}
	return cc.getPreferences(ctx)
}

// getEventsInRange loads events for the given time range.
func (se *SlotEngine) getEventsInRange(ctx database.TenantContext, start, end time.Time) ([]CalendarEvent, error) {
	cc := &ConflictChecker{pool: se.pool}
	return cc.getEventsInRange(ctx, start, end)
}
