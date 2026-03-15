package calendar

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// DetectConflicts runs sort-and-scan O(N log N) conflict detection.
// Returns hard (time overlap), soft (back-to-back < buffer), and preference conflicts.
func DetectConflicts(events []CalendarEvent, prefs *CalendarPreference) []Conflict {
	active := filterActive(events)
	sort.Slice(active, func(i, j int) bool {
		return active[i].StartTime.Before(active[j].StartTime)
	})

	buffer := time.Duration(prefs.BufferMinutes) * time.Minute
	var conflicts []Conflict

	for i := 0; i < len(active); i++ {
		// Check pairs for hard/soft conflicts
		for j := i + 1; j < len(active); j++ {
			// Break early if next event starts after current end + buffer
			if !active[j].StartTime.Before(active[i].EndTime.Add(buffer)) {
				break
			}

			// Skip all-day events from hard/soft conflict pairs
			if active[i].IsAllDay || active[j].IsAllDay {
				continue
			}

			if active[j].StartTime.Before(active[i].EndTime) {
				// Hard conflict: actual time overlap
				overlap := active[i].EndTime.Sub(active[j].StartTime)
				conflicts = append(conflicts, Conflict{
					UserID:     active[i].UserID,
					TenantID:   active[i].TenantID,
					EventAID:   active[i].ID,
					EventBID:   &active[j].ID,
					Type:       "hard",
					Severity:   "critical",
					OverlapMin: int(overlap.Minutes()),
					Detail:     fmt.Sprintf("%q overlaps with %q by %d minutes", active[i].Title, active[j].Title, int(overlap.Minutes())),
				})
			} else {
				// Soft conflict: gap < buffer
				gap := active[j].StartTime.Sub(active[i].EndTime)
				conflicts = append(conflicts, Conflict{
					UserID:   active[i].UserID,
					TenantID: active[i].TenantID,
					EventAID: active[i].ID,
					EventBID: &active[j].ID,
					Type:     "soft",
					Severity: "warn",
					GapMin:   int(gap.Minutes()),
					Detail:   fmt.Sprintf("Only %d min gap between %q and %q (prefer %d min buffer)", int(gap.Minutes()), active[i].Title, active[j].Title, prefs.BufferMinutes),
				})
			}
		}

		// Preference conflicts
		if violations := checkPreferences(active[i], prefs); len(violations) > 0 {
			for _, v := range violations {
				conflicts = append(conflicts, Conflict{
					UserID:   active[i].UserID,
					TenantID: active[i].TenantID,
					EventAID: active[i].ID,
					Type:     "preference",
					Severity: "info",
					Detail:   v,
				})
			}
		}
	}

	return conflicts
}

// filterActive excludes cancelled events.
func filterActive(events []CalendarEvent) []CalendarEvent {
	active := make([]CalendarEvent, 0, len(events))
	for _, e := range events {
		if e.Status != "cancelled" {
			active = append(active, e)
		}
	}
	return active
}

// checkPreferences detects events violating user preferences.
func checkPreferences(event CalendarEvent, prefs *CalendarPreference) []string {
	var violations []string

	// Skip all-day events for hour-based checks
	if event.IsAllDay {
		return violations
	}

	hour := event.StartTime.Hour()
	endHour := event.EndTime.Hour()

	// Outside preferred hours
	if hour < prefs.PreferredStartHour {
		violations = append(violations, fmt.Sprintf("%q starts at %d:00, before preferred start %d:00", event.Title, hour, prefs.PreferredStartHour))
	}
	if endHour > prefs.PreferredEndHour {
		violations = append(violations, fmt.Sprintf("%q ends at %d:00, after preferred end %d:00", event.Title, endHour, prefs.PreferredEndHour))
	}

	// No-meeting day
	var noMeetingDays []int
	if prefs.NoMeetingDays != nil {
		_ = json.Unmarshal(prefs.NoMeetingDays, &noMeetingDays)
	}
	weekday := int(event.StartTime.Weekday())
	for _, day := range noMeetingDays {
		if weekday == day {
			violations = append(violations, fmt.Sprintf("%q scheduled on no-meeting day (%s)", event.Title, event.StartTime.Weekday().String()))
			break
		}
	}

	// Focus block overlap
	var focusBlocks []FocusBlock
	if prefs.FocusBlocks != nil {
		_ = json.Unmarshal(prefs.FocusBlocks, &focusBlocks)
	}
	for _, fb := range focusBlocks {
		fbStart, err1 := time.Parse("15:04", fb.Start)
		fbEnd, err2 := time.Parse("15:04", fb.End)
		if err1 != nil || err2 != nil {
			continue
		}
		eventStart := event.StartTime.Hour()*60 + event.StartTime.Minute()
		eventEnd := event.EndTime.Hour()*60 + event.EndTime.Minute()
		blockStart := fbStart.Hour()*60 + fbStart.Minute()
		blockEnd := fbEnd.Hour()*60 + fbEnd.Minute()

		if eventStart < blockEnd && eventEnd > blockStart {
			violations = append(violations, fmt.Sprintf("%q overlaps with focus block %q (%s-%s)", event.Title, fb.Label, fb.Start, fb.End))
		}
	}

	return violations
}
