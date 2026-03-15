package calendar

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScoreSlot(t *testing.T) {
	t.Parallel()

	defaultPrefs := &CalendarPreference{
		PreferredStartHour: 9,
		PreferredEndHour:   18,
		BufferMinutes:      15,
		NoMeetingDays:      json.RawMessage("[]"),
		FocusBlocks:        json.RawMessage("[]"),
		HomeTimezone:       "UTC",
	}

	base := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC) // Monday

	t.Run("all free - high attendee confidence", func(t *testing.T) {
		t.Parallel()
		slot := TimeSlot{
			Start: base.Add(10 * time.Hour),
			End:   base.Add(11 * time.Hour),
		}
		input := ScoreInput{
			UserPrefs: defaultPrefs,
			Attendees: []string{},
		}
		result := ScoreSlot(slot, input)
		if result.Breakdown.AttendeeConfidence < 0.9 {
			t.Errorf("expected high attendee confidence with no attendees, got %.2f", result.Breakdown.AttendeeConfidence)
		}
	})

	t.Run("within business hours", func(t *testing.T) {
		t.Parallel()
		slot := TimeSlot{
			Start: base.Add(10 * time.Hour),
			End:   base.Add(11 * time.Hour),
		}
		input := ScoreInput{UserPrefs: defaultPrefs}
		result := ScoreSlot(slot, input)
		if result.Breakdown.BusinessHours < 0.9 {
			t.Errorf("expected high business hours score, got %.2f", result.Breakdown.BusinessHours)
		}
	})

	t.Run("outside business hours", func(t *testing.T) {
		t.Parallel()
		slot := TimeSlot{
			Start: base.Add(7 * time.Hour),
			End:   base.Add(8 * time.Hour),
		}
		input := ScoreInput{UserPrefs: defaultPrefs}
		result := ScoreSlot(slot, input)
		if result.Breakdown.BusinessHours > 0.5 {
			t.Errorf("expected low business hours score for 7-8 AM, got %.2f", result.Breakdown.BusinessHours)
		}
	})

	t.Run("call prefers morning", func(t *testing.T) {
		t.Parallel()
		morningSlot := TimeSlot{Start: base.Add(10 * time.Hour), End: base.Add(10*time.Hour + 30*time.Minute)}
		afternoonSlot := TimeSlot{Start: base.Add(15 * time.Hour), End: base.Add(15*time.Hour + 30*time.Minute)}
		input := ScoreInput{UserPrefs: defaultPrefs, MeetingType: "call_30m"}

		morningScore := ScoreSlot(morningSlot, input)
		afternoonScore := ScoreSlot(afternoonSlot, input)

		if morningScore.Breakdown.TimeOfDay <= afternoonScore.Breakdown.TimeOfDay {
			t.Errorf("morning should score higher than afternoon for calls: morning=%.2f, afternoon=%.2f",
				morningScore.Breakdown.TimeOfDay, afternoonScore.Breakdown.TimeOfDay)
		}
	})

	t.Run("total score between 0 and 1", func(t *testing.T) {
		t.Parallel()
		slot := TimeSlot{Start: base.Add(10 * time.Hour), End: base.Add(11 * time.Hour)}
		input := ScoreInput{UserPrefs: defaultPrefs}
		result := ScoreSlot(slot, input)
		if result.Score < 0 || result.Score > 1 {
			t.Errorf("total score should be between 0 and 1, got %.2f", result.Score)
		}
	})

	t.Run("focus block penalizes preference score", func(t *testing.T) {
		t.Parallel()
		prefsWithFocus := &CalendarPreference{
			PreferredStartHour: 9,
			PreferredEndHour:   18,
			BufferMinutes:      15,
			NoMeetingDays:      json.RawMessage("[]"),
			FocusBlocks:        json.RawMessage(`[{"start":"10:00","end":"12:00","label":"Deep Work"}]`),
			HomeTimezone:       "UTC",
		}
		slot := TimeSlot{Start: base.Add(10 * time.Hour), End: base.Add(11 * time.Hour)}
		input := ScoreInput{UserPrefs: prefsWithFocus}
		result := ScoreSlot(slot, input)
		if result.Breakdown.UserPreference >= 0.8 {
			t.Errorf("expected low preference score during focus block, got %.2f", result.Breakdown.UserPreference)
		}
	})

	t.Run("proximity to proposed time", func(t *testing.T) {
		t.Parallel()
		proposedTime := base.Add(10 * time.Hour)
		nearSlot := TimeSlot{Start: base.Add(10 * time.Hour), End: base.Add(11 * time.Hour)}
		farSlot := TimeSlot{Start: base.Add(16 * time.Hour), End: base.Add(17 * time.Hour)}
		inputNear := ScoreInput{UserPrefs: defaultPrefs, ProposedTimes: []time.Time{proposedTime}}
		inputFar := ScoreInput{UserPrefs: defaultPrefs, ProposedTimes: []time.Time{proposedTime}}

		nearScore := ScoreSlot(nearSlot, inputNear)
		farScore := ScoreSlot(farSlot, inputFar)

		if nearScore.Breakdown.ProximityToProposed <= farScore.Breakdown.ProximityToProposed {
			t.Errorf("near slot should score higher proximity: near=%.2f, far=%.2f",
				nearScore.Breakdown.ProximityToProposed, farScore.Breakdown.ProximityToProposed)
		}
	})

	t.Run("reasoning includes explanation", func(t *testing.T) {
		t.Parallel()
		slot := TimeSlot{Start: base.Add(10 * time.Hour), End: base.Add(11 * time.Hour)}
		input := ScoreInput{UserPrefs: defaultPrefs}
		result := ScoreSlot(slot, input)
		if result.Reasoning() == "" {
			t.Error("expected non-empty reasoning")
		}
	})
}
