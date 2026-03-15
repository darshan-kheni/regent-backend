package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip Re: prefix",
			input:    "Re: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "strip Fwd: prefix",
			input:    "Fwd: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "strip FW: prefix",
			input:    "FW: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "strip RE: prefix",
			input:    "RE: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "nested Re: Re: Fwd: prefixes",
			input:    "Re: Re: Fwd: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "deeply nested prefixes",
			input:    "Re: Fwd: Re: FW: RE: Important topic",
			expected: "Important topic",
		},
		{
			name:     "whitespace after prefix",
			input:    "Re:   Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "no space after colon",
			input:    "Re:Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only prefix",
			input:    "Re:",
			expected: "",
		},
		{
			name:     "only nested prefixes",
			input:    "Re: Fwd: Re:",
			expected: "",
		},
		{
			name:     "leading and trailing whitespace",
			input:    "  Re: Meeting tomorrow  ",
			expected: "Meeting tomorrow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeSubject(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeSubject_NoPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain subject unchanged",
			input:    "Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "subject with colon but not prefix",
			input:    "Important: please review",
			expected: "Important: please review",
		},
		{
			name:     "subject starting with similar but not prefix",
			input:    "Regarding the proposal",
			expected: "Regarding the proposal",
		},
		{
			name:     "subject with Re in middle",
			input:    "This is Re: something",
			expected: "This is Re: something",
		},
		{
			name:     "subject with numbers",
			input:    "Q4 2025 Results",
			expected: "Q4 2025 Results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeSubject(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeSubject_CaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase re:",
			input:    "re: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "uppercase RE:",
			input:    "RE: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "mixed case Re:",
			input:    "Re: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "lowercase fwd:",
			input:    "fwd: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "uppercase FWD:",
			input:    "FWD: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "mixed case Fwd:",
			input:    "Fwd: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "lowercase fw:",
			input:    "fw: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "uppercase FW:",
			input:    "FW: Meeting tomorrow",
			expected: "Meeting tomorrow",
		},
		{
			name:     "mixed case nested",
			input:    "re: FWD: Re: fw: Topic",
			expected: "Topic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeSubject(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
