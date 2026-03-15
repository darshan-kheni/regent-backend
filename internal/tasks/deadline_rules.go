package tasks

import (
	"regexp"
	"strings"
	"time"
)

// BusinessRules handles ASAP/EOD/COB/EOW/EOM deadline text.
type BusinessRules struct{}

// NewBusinessRules creates a new BusinessRules parser.
func NewBusinessRules() *BusinessRules {
	return &BusinessRules{}
}

var asapPatterns = regexp.MustCompile(`(?i)\b(asap|immediately|right away|right now|as soon as possible)\b`)
var eodPatterns = regexp.MustCompile(`(?i)\b(eod|end of day|end of the day|by end of day|by eod)\b`)
var cobPatterns = regexp.MustCompile(`(?i)\b(cob|close of business|by close of business|by cob)\b`)
var eowPatterns = regexp.MustCompile(`(?i)\b(eow|end of week|end of the week|by end of week|by eow)\b`)
var eomPatterns = regexp.MustCompile(`(?i)\b(eom|end of month|end of the month|by end of month|by eom)\b`)
var tomorrowPattern = regexp.MustCompile(`(?i)\b(tomorrow|by tomorrow)\b`)

// Parse tries to match business shorthand in the text.
func (b *BusinessRules) Parse(text string, ref time.Time) *time.Time {
	lower := strings.ToLower(text)
	_ = lower

	if asapPatterns.MatchString(text) {
		t := ref.Add(24 * time.Hour)
		return &t
	}
	if eodPatterns.MatchString(text) {
		t := time.Date(ref.Year(), ref.Month(), ref.Day(), 18, 0, 0, 0, ref.Location())
		if t.Before(ref) {
			t = t.Add(24 * time.Hour)
		}
		return &t
	}
	if cobPatterns.MatchString(text) {
		t := time.Date(ref.Year(), ref.Month(), ref.Day(), 17, 0, 0, 0, ref.Location())
		if t.Before(ref) {
			t = t.Add(24 * time.Hour)
		}
		return &t
	}
	if eowPatterns.MatchString(text) {
		daysUntilFriday := (5 - int(ref.Weekday()) + 7) % 7
		if daysUntilFriday == 0 {
			daysUntilFriday = 7
		}
		friday := ref.AddDate(0, 0, daysUntilFriday)
		t := time.Date(friday.Year(), friday.Month(), friday.Day(), 18, 0, 0, 0, ref.Location())
		return &t
	}
	if eomPatterns.MatchString(text) {
		firstOfNext := time.Date(ref.Year(), ref.Month()+1, 1, 0, 0, 0, 0, ref.Location())
		lastDay := firstOfNext.Add(-24 * time.Hour)
		t := time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 18, 0, 0, 0, ref.Location())
		return &t
	}
	if tomorrowPattern.MatchString(text) {
		tomorrow := ref.Add(24 * time.Hour)
		t := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 18, 0, 0, 0, ref.Location())
		return &t
	}

	return nil
}
