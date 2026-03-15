package briefings

import (
	"encoding/json"
	"strings"
	"time"
)

// Matches checks if a rule's condition matches the given briefing.
func (r *Rule) Matches(b Briefing) bool {
	switch r.RuleType {
	case "vip":
		return r.matchVIP(b)
	case "sender":
		return r.matchSender(b)
	case "keyword":
		return r.matchKeyword(b)
	case "category":
		return r.matchCategory(b)
	case "time":
		return r.matchTime(b)
	default:
		return false
	}
}

// VIP condition: {"emails": ["boss@company.com"], "domains": ["vip-client.com"]}
func (r *Rule) matchVIP(b Briefing) bool {
	var cond struct {
		Emails  []string `json:"emails"`
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(r.Condition, &cond); err != nil {
		return false
	}

	senderLower := strings.ToLower(b.SenderName)

	for _, email := range cond.Emails {
		if strings.ToLower(email) == senderLower {
			return true
		}
	}
	for _, domain := range cond.Domains {
		if strings.HasSuffix(senderLower, "@"+strings.ToLower(domain)) {
			return true
		}
	}
	return false
}

// Sender condition: {"email": "alice@example.com"} or {"domain": "example.com"}
func (r *Rule) matchSender(b Briefing) bool {
	var cond struct {
		Email  string `json:"email"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(r.Condition, &cond); err != nil {
		return false
	}

	senderLower := strings.ToLower(b.SenderName)

	if cond.Email != "" && strings.ToLower(cond.Email) == senderLower {
		return true
	}
	if cond.Domain != "" && strings.HasSuffix(senderLower, "@"+strings.ToLower(cond.Domain)) {
		return true
	}
	return false
}

// Keyword condition: {"terms": ["invoice", "deadline"], "match": "subject|body|both"}
func (r *Rule) matchKeyword(b Briefing) bool {
	var cond struct {
		Terms []string `json:"terms"`
		Match string   `json:"match"` // subject, body, both
	}
	if err := json.Unmarshal(r.Condition, &cond); err != nil {
		return false
	}

	if cond.Match == "" {
		cond.Match = "both"
	}

	for _, term := range cond.Terms {
		termLower := strings.ToLower(term)
		switch cond.Match {
		case "subject":
			if strings.Contains(strings.ToLower(b.Subject), termLower) {
				return true
			}
		case "body":
			if strings.Contains(strings.ToLower(b.Summary), termLower) {
				return true
			}
		default: // "both"
			if strings.Contains(strings.ToLower(b.Subject), termLower) ||
				strings.Contains(strings.ToLower(b.Summary), termLower) {
				return true
			}
		}
	}
	return false
}

// Category condition: {"categories": ["Legal", "Finance", "Urgent"]}
func (r *Rule) matchCategory(b Briefing) bool {
	var cond struct {
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal(r.Condition, &cond); err != nil {
		return false
	}

	catLower := strings.ToLower(b.Category)
	for _, cat := range cond.Categories {
		if strings.ToLower(cat) == catLower {
			return true
		}
	}
	return false
}

// Time condition (quiet hours): {"start": "22:00", "end": "07:00", "timezone": "America/New_York"}
// Returns true if current time is within quiet hours (i.e., the rule matches).
func (r *Rule) matchTime(b Briefing) bool {
	var cond struct {
		Start    string `json:"start"`    // "22:00"
		End      string `json:"end"`      // "07:00"
		Timezone string `json:"timezone"` // "America/New_York"
	}
	if err := json.Unmarshal(r.Condition, &cond); err != nil {
		return false
	}

	if cond.Timezone == "" {
		cond.Timezone = "UTC"
	}

	loc, err := time.LoadLocation(cond.Timezone)
	if err != nil {
		return false
	}

	now := time.Now().In(loc)
	startH, startM := parseHourMinute(cond.Start)
	endH, endM := parseHourMinute(cond.End)

	nowMinutes := now.Hour()*60 + now.Minute()
	startMinutes := startH*60 + startM
	endMinutes := endH*60 + endM

	if startMinutes <= endMinutes {
		// Same-day window (e.g., 09:00 - 17:00)
		return nowMinutes >= startMinutes && nowMinutes < endMinutes
	}
	// Overnight window (e.g., 22:00 - 07:00)
	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}

// parseHourMinute parses "HH:MM" into hour and minute integers.
func parseHourMinute(s string) (int, int) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	h, m := 0, 0
	for _, c := range parts[0] {
		h = h*10 + int(c-'0')
	}
	for _, c := range parts[1] {
		m = m*10 + int(c-'0')
	}
	return h, m
}
