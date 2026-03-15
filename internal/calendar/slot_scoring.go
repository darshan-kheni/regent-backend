package calendar

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Score weights for 5 dimensions (must sum to 1.0).
var weights = [5]float64{0.25, 0.25, 0.20, 0.20, 0.10}

// ScoreInput provides context for scoring a slot.
type ScoreInput struct {
	UserPrefs     *CalendarPreference
	Events        []CalendarEvent
	MeetingType   string
	ProposedTimes []time.Time
	Attendees     []string
}

// ScoredSlot wraps a TimeSlot with its score and breakdown.
type ScoredSlot struct {
	Slot      TimeSlot
	Score     float64
	Breakdown ScoreBreakdown
}

// Reasoning generates a human-readable explanation of the score.
func (s ScoredSlot) Reasoning() string {
	parts := []string{}
	if s.Breakdown.BusinessHours >= 0.8 {
		parts = append(parts, "within business hours")
	}
	if s.Breakdown.UserPreference >= 0.8 {
		parts = append(parts, "matches your preferences")
	}
	if s.Breakdown.AttendeeConfidence >= 0.8 {
		parts = append(parts, "all attendees likely available")
	}
	if s.Breakdown.TimeOfDay >= 0.7 {
		parts = append(parts, "good time of day for this meeting type")
	}
	if len(parts) == 0 {
		parts = append(parts, "available slot")
	}
	return fmt.Sprintf("Score: %.0f%% — %s", s.Score*100, strings.Join(parts, ", "))
}

// ScoreSlot calculates a weighted score for a candidate slot.
func ScoreSlot(slot TimeSlot, input ScoreInput) ScoredSlot {
	bd := ScoreBreakdown{
		BusinessHours:       scoreBusinessHours(slot, input.UserPrefs),
		UserPreference:      scorePreference(slot, input.UserPrefs),
		TimeOfDay:           scoreTimeOfDay(slot, input.MeetingType),
		AttendeeConfidence:  scoreAttendeeConfidence(input.Attendees, input.Events, slot),
		ProximityToProposed: scoreProximity(slot, input.ProposedTimes),
	}

	total := bd.BusinessHours*weights[0] +
		bd.UserPreference*weights[1] +
		bd.TimeOfDay*weights[2] +
		bd.AttendeeConfidence*weights[3] +
		bd.ProximityToProposed*weights[4]

	return ScoredSlot{Slot: slot, Score: total, Breakdown: bd}
}

// scoreBusinessHours: 1.0 if fully within preferred hours, scaled down otherwise.
func scoreBusinessHours(slot TimeSlot, prefs *CalendarPreference) float64 {
	startHour := slot.Start.Hour()
	endHour := slot.End.Hour()
	if slot.End.Minute() > 0 {
		endHour++
	}

	if startHour >= prefs.PreferredStartHour && endHour <= prefs.PreferredEndHour {
		return 1.0
	}

	// Partial credit based on how much is within hours
	totalMin := slot.End.Sub(slot.Start).Minutes()
	withinStart := max(startHour, prefs.PreferredStartHour)
	withinEnd := min(endHour, prefs.PreferredEndHour)
	if withinEnd <= withinStart {
		return 0.0
	}
	withinMin := float64((withinEnd - withinStart) * 60)
	return withinMin / totalMin
}

// scorePreference: avoids focus blocks, prefers buffer around meetings.
func scorePreference(slot TimeSlot, prefs *CalendarPreference) float64 {
	score := 1.0

	// Check focus blocks
	var focusBlocks []FocusBlock
	if prefs.FocusBlocks != nil {
		_ = json.Unmarshal(prefs.FocusBlocks, &focusBlocks)
	}
	slotStartMin := slot.Start.Hour()*60 + slot.Start.Minute()
	slotEndMin := slot.End.Hour()*60 + slot.End.Minute()

	for _, fb := range focusBlocks {
		fbStart, err1 := time.Parse("15:04", fb.Start)
		fbEnd, err2 := time.Parse("15:04", fb.End)
		if err1 != nil || err2 != nil {
			continue
		}
		blockStartMin := fbStart.Hour()*60 + fbStart.Minute()
		blockEndMin := fbEnd.Hour()*60 + fbEnd.Minute()
		if slotStartMin < blockEndMin && slotEndMin > blockStartMin {
			score -= 0.5 // Heavy penalty for focus block overlap
		}
	}

	if score < 0 {
		score = 0
	}
	return score
}

// scoreTimeOfDay: morning preferred for calls, afternoon for workshops.
func scoreTimeOfDay(slot TimeSlot, meetingType string) float64 {
	hour := slot.Start.Hour()

	switch meetingType {
	case "call_30m":
		// Prefer morning (9-12)
		if hour >= 9 && hour < 12 {
			return 1.0
		}
		if hour >= 12 && hour < 14 {
			return 0.7
		}
		return 0.5
	case "workshop_2h":
		// Prefer afternoon (13-17)
		if hour >= 13 && hour < 16 {
			return 1.0
		}
		if hour >= 10 && hour < 13 {
			return 0.7
		}
		return 0.5
	default:
		// meeting_1h, custom — slight morning preference
		if hour >= 10 && hour < 14 {
			return 1.0
		}
		if hour >= 9 && hour < 16 {
			return 0.8
		}
		return 0.6
	}
}

// scoreAttendeeConfidence: if all attendees are internal and free -> 1.0, unknown -> 0.49.
func scoreAttendeeConfidence(attendees []string, _ []CalendarEvent, _ TimeSlot) float64 {
	if len(attendees) == 0 {
		return 1.0 // No attendees to check
	}

	totalScore := 0.0
	for range attendees {
		// For now, without FreeBusy integration, assume unknown availability
		totalScore += 0.49
	}
	return totalScore / float64(len(attendees))
}

// scoreProximity: how close the slot is to any proposed times.
func scoreProximity(slot TimeSlot, proposed []time.Time) float64 {
	if len(proposed) == 0 {
		return 0.5 // No proposed times — neutral score
	}

	minDist := math.MaxFloat64
	for _, p := range proposed {
		dist := math.Abs(slot.Start.Sub(p).Hours())
		if dist < minDist {
			minDist = dist
		}
	}

	// Scale: 0 hours = 1.0, 24+ hours = 0.0
	if minDist >= 24 {
		return 0.0
	}
	return 1.0 - (minDist / 24.0)
}
