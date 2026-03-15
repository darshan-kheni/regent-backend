package calendar

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/tj/go-naturaldate"
)

// ParseSchedulingTime attempts to parse a natural language date/time string
// using a 3-layer approach: custom regex → go-naturaldate → dateparse.
func ParseSchedulingTime(text string, ref time.Time) (*DateTimeResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty date/time text")
	}

	// Layer 1: Custom regex for time ranges and named periods
	if result, ok := parseTimeRange(text, ref); ok {
		return result, nil
	}

	// Layer 2: dateparse for explicit dates ("March 15 2026", "3/15/2026")
	// Try this before naturaldate since it's stricter about parsing.
	if t, err := dateparse.ParseIn(text, ref.Location()); err == nil {
		return &DateTimeResult{Start: t, HasTime: containsTimeToken(text)}, nil
	}

	// Layer 3: go-naturaldate for relative expressions ("next Tuesday", "tomorrow")
	// naturaldate can return ref unchanged for unparseable input, so we validate
	// that the result differs from the reference time.
	if t, err := naturaldate.Parse(text, ref, naturaldate.WithDirection(naturaldate.Future)); err == nil {
		if !t.Equal(ref) {
			return &DateTimeResult{Start: t, HasTime: containsTimeToken(text)}, nil
		}
	}

	return nil, fmt.Errorf("could not parse date/time: %q", text)
}

// parseTimeRange handles named periods and numeric time ranges.
func parseTimeRange(text string, ref time.Time) (*DateTimeResult, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Named periods
	switch {
	case lower == "morning" || strings.HasSuffix(lower, " morning"):
		start := time.Date(ref.Year(), ref.Month(), ref.Day(), 9, 0, 0, 0, ref.Location())
		end := time.Date(ref.Year(), ref.Month(), ref.Day(), 12, 0, 0, 0, ref.Location())
		return &DateTimeResult{Start: start, End: &end, HasTime: true}, true
	case lower == "afternoon" || strings.HasSuffix(lower, " afternoon"):
		start := time.Date(ref.Year(), ref.Month(), ref.Day(), 13, 0, 0, 0, ref.Location())
		end := time.Date(ref.Year(), ref.Month(), ref.Day(), 17, 0, 0, 0, ref.Location())
		return &DateTimeResult{Start: start, End: &end, HasTime: true}, true
	case lower == "evening" || strings.HasSuffix(lower, " evening"):
		start := time.Date(ref.Year(), ref.Month(), ref.Day(), 17, 0, 0, 0, ref.Location())
		end := time.Date(ref.Year(), ref.Month(), ref.Day(), 20, 0, 0, 0, ref.Location())
		return &DateTimeResult{Start: start, End: &end, HasTime: true}, true
	case lower == "eod" || lower == "end of day":
		start := time.Date(ref.Year(), ref.Month(), ref.Day(), 17, 0, 0, 0, ref.Location())
		end := time.Date(ref.Year(), ref.Month(), ref.Day(), 18, 0, 0, 0, ref.Location())
		return &DateTimeResult{Start: start, End: &end, HasTime: true}, true
	}

	// Numeric ranges: "between 2-4 PM", "2pm to 4pm", "2:00-4:00 PM"
	rangePattern := regexp.MustCompile(`(?i)(?:between\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:to|-|–)\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := rangePattern.FindStringSubmatch(text); len(matches) > 0 {
		startHour, _ := strconv.Atoi(matches[1])
		startMin := 0
		if matches[2] != "" {
			startMin, _ = strconv.Atoi(matches[2])
		}
		endHour, _ := strconv.Atoi(matches[4])
		endMin := 0
		if matches[5] != "" {
			endMin, _ = strconv.Atoi(matches[5])
		}

		// Determine AM/PM
		meridiem := strings.ToLower(matches[6]) // end meridiem takes priority
		if meridiem == "" {
			meridiem = strings.ToLower(matches[3])
		}
		if meridiem == "pm" {
			if endHour < 12 {
				endHour += 12
			}
			if startHour < 12 && startHour < endHour-12 {
				startHour += 12
			}
		} else if meridiem == "am" {
			if startHour == 12 {
				startHour = 0
			}
			if endHour == 12 {
				endHour = 0
			}
		}

		start := time.Date(ref.Year(), ref.Month(), ref.Day(), startHour, startMin, 0, 0, ref.Location())
		end := time.Date(ref.Year(), ref.Month(), ref.Day(), endHour, endMin, 0, 0, ref.Location())
		return &DateTimeResult{Start: start, End: &end, HasTime: true}, true
	}

	return nil, false
}

// containsTimeToken checks if the text contains time-specific tokens.
func containsTimeToken(text string) bool {
	timePattern := regexp.MustCompile(`(?i)(\d{1,2}:\d{2}|\d{1,2}\s*(am|pm)|morning|afternoon|evening|noon|midnight)`)
	return timePattern.MatchString(text)
}
