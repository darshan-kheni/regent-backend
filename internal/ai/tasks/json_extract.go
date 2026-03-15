package tasks

import "strings"

// extractJSON extracts the first JSON object from an AI response that may
// contain surrounding text, markdown fences, or explanatory prose.
// Falls back to the original string if no JSON object is found.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Strip markdown code fences if present
	if idx := strings.Index(s, "```json"); idx != -1 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end != -1 {
			return strings.TrimSpace(s[:end])
		}
	}
	if idx := strings.Index(s, "```"); idx != -1 {
		after := s[idx+3:]
		if end := strings.Index(after, "```"); end != -1 {
			return strings.TrimSpace(after[:end])
		}
	}

	// Find the first '{' and match it to its closing '}'
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return s
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	// No balanced closing brace found — return from first '{'
	return s[start:]
}
