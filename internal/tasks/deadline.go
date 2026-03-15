package tasks

import (
	"regexp"
	"strings"
	"time"

	"github.com/olebedev/when"
	"github.com/olebedev/when/rules/en"
)

// DeadlineParser orchestrates deadline parsing through a 3-layer chain:
// 1. Custom business rules (ASAP/EOD/COB/EOW/EOM)
// 2. olebedev/when (natural language prose scanning)
// 3. Regex fallback for explicit date patterns
type DeadlineParser struct {
	rules *BusinessRules
	when  *when.Parser
}

// NewDeadlineParser creates a new DeadlineParser.
func NewDeadlineParser() *DeadlineParser {
	w := when.New(nil)
	w.Add(en.All...)

	return &DeadlineParser{
		rules: NewBusinessRules(),
		when:  w,
	}
}

// Parse attempts to extract a deadline from text using a 3-layer chain.
// Returns nil if no deadline could be parsed (needs_confirmation should be set).
func (p *DeadlineParser) Parse(text string, emailTime time.Time) *time.Time {
	if text == "" {
		return nil
	}

	// Layer 1: Custom business rules
	if t := p.rules.Parse(text, emailTime); t != nil {
		return t
	}

	// Layer 2: olebedev/when natural language parsing
	result, err := p.when.Parse(text, emailTime)
	if err == nil && result != nil {
		t := result.Time
		return &t
	}

	// Layer 3: Regex fallback for explicit dates
	if t := p.regexParse(text, emailTime); t != nil {
		return t
	}

	return nil
}

// Common date patterns for regex fallback
var (
	// Matches: 2026-03-15, 2026/03/15
	isoDatePattern = regexp.MustCompile(`\b(\d{4})[-/](\d{1,2})[-/](\d{1,2})\b`)
	// Matches: 3/15/2026, 03/15/2026
	usDatePattern = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4})\b`)
	// Matches: March 15, Mar 15, March 15th
	monthDayPattern = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
)

var monthNames = map[string]time.Month{
	"january": time.January, "jan": time.January,
	"february": time.February, "feb": time.February,
	"march": time.March, "mar": time.March,
	"april": time.April, "apr": time.April,
	"may": time.May,
	"june": time.June, "jun": time.June,
	"july": time.July, "jul": time.July,
	"august": time.August, "aug": time.August,
	"september": time.September, "sep": time.September,
	"october": time.October, "oct": time.October,
	"november": time.November, "nov": time.November,
	"december": time.December, "dec": time.December,
}

func (p *DeadlineParser) regexParse(text string, ref time.Time) *time.Time {
	// Try ISO date: 2026-03-15
	if m := isoDatePattern.FindStringSubmatch(text); m != nil {
		t, err := time.Parse("2006-1-2", m[1]+"-"+m[2]+"-"+m[3])
		if err == nil {
			t = time.Date(t.Year(), t.Month(), t.Day(), 18, 0, 0, 0, ref.Location())
			return &t
		}
	}

	// Try US date: 3/15/2026
	if m := usDatePattern.FindStringSubmatch(text); m != nil {
		t, err := time.Parse("1/2/2006", m[1]+"/"+m[2]+"/"+m[3])
		if err == nil {
			t = time.Date(t.Year(), t.Month(), t.Day(), 18, 0, 0, 0, ref.Location())
			return &t
		}
	}

	// Try month name: March 15
	if m := monthDayPattern.FindStringSubmatch(text); m != nil {
		month, ok := monthNames[strings.ToLower(m[1])]
		if ok {
			day := 0
			for _, c := range m[2] {
				day = day*10 + int(c-'0')
			}
			year := ref.Year()
			t := time.Date(year, month, day, 18, 0, 0, 0, ref.Location())
			// If the date is in the past, assume next year
			if t.Before(ref) {
				t = t.AddDate(1, 0, 0)
			}
			return &t
		}
	}

	return nil
}
