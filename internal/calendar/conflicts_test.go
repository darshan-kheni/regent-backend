package calendar

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDetectConflicts(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	tenantID := uuid.New()
	defaultPrefs := &CalendarPreference{
		PreferredStartHour: 9,
		PreferredEndHour:   18,
		BufferMinutes:      15,
		NoMeetingDays:      json.RawMessage("[]"),
		FocusBlocks:        json.RawMessage("[]"),
		HomeTimezone:       "UTC",
	}

	makeEvent := func(title string, start, end time.Time, opts ...func(*CalendarEvent)) CalendarEvent {
		e := CalendarEvent{
			ID: uuid.New(), UserID: userID, TenantID: tenantID,
			Title: title, StartTime: start, EndTime: end,
			Status: "confirmed",
		}
		for _, o := range opts {
			o(&e)
		}
		return e
	}

	base := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC) // Monday

	t.Run("hard overlap", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("Meeting A", base.Add(9*time.Hour), base.Add(10*time.Hour)),
			makeEvent("Meeting B", base.Add(9*time.Hour+30*time.Minute), base.Add(10*time.Hour+30*time.Minute)),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		hardCount := 0
		for _, c := range conflicts {
			if c.Type == "hard" {
				hardCount++
				if c.OverlapMin != 30 {
					t.Errorf("expected 30 min overlap, got %d", c.OverlapMin)
				}
			}
		}
		if hardCount == 0 {
			t.Error("expected at least one hard conflict")
		}
	})

	t.Run("soft back-to-back", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("Meeting A", base.Add(9*time.Hour), base.Add(10*time.Hour)),
			makeEvent("Meeting B", base.Add(10*time.Hour), base.Add(11*time.Hour)),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		softCount := 0
		for _, c := range conflicts {
			if c.Type == "soft" {
				softCount++
			}
		}
		if softCount == 0 {
			t.Error("expected at least one soft conflict for back-to-back meetings")
		}
	})

	t.Run("preference violation - early morning", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("Early Meeting", base.Add(7*time.Hour), base.Add(8*time.Hour)),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		prefCount := 0
		for _, c := range conflicts {
			if c.Type == "preference" {
				prefCount++
			}
		}
		if prefCount == 0 {
			t.Error("expected preference conflict for 7 AM meeting with preferred start 9")
		}
	})

	t.Run("no conflict with buffer", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("Meeting A", base.Add(9*time.Hour), base.Add(10*time.Hour)),
			makeEvent("Meeting B", base.Add(10*time.Hour+20*time.Minute), base.Add(11*time.Hour)),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		for _, c := range conflicts {
			if c.Type == "hard" || c.Type == "soft" {
				t.Error("expected no hard/soft conflict with 20 min gap and 15 min buffer")
			}
		}
	})

	t.Run("cross provider", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("Google Meeting", base.Add(14*time.Hour), base.Add(15*time.Hour), func(e *CalendarEvent) { e.Provider = "google" }),
			makeEvent("MS Meeting", base.Add(14*time.Hour+30*time.Minute), base.Add(15*time.Hour+30*time.Minute), func(e *CalendarEvent) { e.Provider = "microsoft" }),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		hardCount := 0
		for _, c := range conflicts {
			if c.Type == "hard" {
				hardCount++
			}
		}
		if hardCount == 0 {
			t.Error("expected hard conflict between cross-provider events")
		}
	})

	t.Run("cancelled ignored", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("Active Meeting", base.Add(9*time.Hour), base.Add(10*time.Hour)),
			makeEvent("Cancelled Meeting", base.Add(9*time.Hour), base.Add(10*time.Hour), func(e *CalendarEvent) { e.Status = "cancelled" }),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		for _, c := range conflicts {
			if c.Type == "hard" {
				t.Error("expected no hard conflict with cancelled event")
			}
		}
	})

	t.Run("all-day skipped from hard/soft", func(t *testing.T) {
		events := []CalendarEvent{
			makeEvent("All Day Event", base, base.Add(24*time.Hour), func(e *CalendarEvent) { e.IsAllDay = true }),
			makeEvent("Afternoon Meeting", base.Add(14*time.Hour), base.Add(15*time.Hour)),
		}
		conflicts := DetectConflicts(events, defaultPrefs)
		for _, c := range conflicts {
			if c.Type == "hard" || c.Type == "soft" {
				t.Error("all-day events should not create hard/soft conflicts")
			}
		}
	})
}
