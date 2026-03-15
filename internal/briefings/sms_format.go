package briefings

import (
	"unicode/utf8"
)

// FormatSMS creates an SMS body from a briefing.
// Format: "REGENT: [SenderName]\n[Subject truncated]\n[Summary 1-line]"
func FormatSMS(b Briefing) string {
	sender := truncateStr(b.SenderName, 30)
	subject := truncateStr(b.Subject, 60)
	summary := truncateStr(b.Summary, 80)

	if summary == "" {
		return "REGENT: " + sender + "\n" + subject
	}
	return "REGENT: " + sender + "\n" + subject + "\n" + summary
}

// truncateStr truncates a string to maxLen characters, adding "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// CountSMSSegments counts how many SMS segments a message will use.
// GSM-7: 160 chars single, 153 per segment if multipart.
// UCS-2: 70 chars single, 67 per segment if multipart.
func CountSMSSegments(body string) int {
	if IsGSM7(body) {
		if len(body) <= 160 {
			return 1
		}
		return (len(body) + 152) / 153
	}
	runeCount := utf8.RuneCountInString(body)
	if runeCount <= 70 {
		return 1
	}
	return (runeCount + 66) / 67
}

// IsGSM7 checks if all characters are in the GSM 7-bit default alphabet.
func IsGSM7(s string) bool {
	for _, r := range s {
		if !isGSM7Char(r) {
			return false
		}
	}
	return true
}

// isGSM7Char checks if a rune is in the GSM 7-bit default alphabet.
func isGSM7Char(r rune) bool {
	// GSM 7-bit default alphabet characters
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '@', '\u00a3', '$', '\u00a5', '\u00e8', '\u00e9', '\u00f9', '\u00ec',
		'\u00f2', '\u00c7', '\n', '\u00d8', '\u00f8', '\r', '\u00c5', '\u00e5',
		'\u0394', '_', '\u03a6', '\u0393', '\u039b', '\u03a9', '\u03a0', '\u03a8',
		'\u03a3', '\u0398', '\u039e', ' ', '!', '"', '#', '\u00a4', '%', '&',
		'\'', '(', ')', '*', '+', ',', '-', '.', '/', ':', ';', '<', '=',
		'>', '?', '\u00a1', '\u00c4', '\u00d6', '\u00d1', '\u00dc', '\u00a7',
		'\u00bf', '\u00e4', '\u00f6', '\u00f1', '\u00fc', '\u00e0':
		return true
	}
	return false
}
