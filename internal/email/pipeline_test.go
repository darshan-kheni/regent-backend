package email

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/darshan-kheni/regent/internal/email/mime"
	"github.com/darshan-kheni/regent/internal/email/send"
)

// --- Subject Normalization Pipeline Tests ---

func TestNormalizeSubjectPipeline(t *testing.T) {
	t.Parallel()

	cases := []struct{ input, expected string }{
		{"Re: Meeting tomorrow", "Meeting tomorrow"},
		{"Fwd: Re: FW: Important", "Important"},
		{"No prefix here", "No prefix here"},
		{"", ""},
		{"RE: RE: RE: Deeply nested", "Deeply nested"},
		{"fw: lowercase forward", "lowercase forward"},
		{"  Re: leading whitespace  ", "leading whitespace"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, NormalizeSubject(tc.input))
		})
	}
}

// --- MIME Parse to Email Pipeline Tests ---

func TestMIMEParseToEmail_SimpleText(t *testing.T) {
	t.Parallel()

	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"Message-Id: <abc@example.com>\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello world\r\n"

	parsed, err := mime.Parse(strings.NewReader(raw), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)
	assert.Equal(t, "Test", parsed.Subject)
	assert.Equal(t, "abc@example.com", parsed.MessageID)
	assert.Equal(t, "sender@example.com", parsed.From)
	assert.Contains(t, parsed.To, "recipient@example.com")
	assert.Contains(t, parsed.TextBody, "Hello world")
}

func TestMIMEParseToEmail_ThreadingHeaders(t *testing.T) {
	t.Parallel()

	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Re: Original subject\r\n" +
		"Message-Id: <reply@example.com>\r\n" +
		"In-Reply-To: <original@example.com>\r\n" +
		"References: <first@example.com> <original@example.com>\r\n" +
		"Date: Tue, 02 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Reply body\r\n"

	parsed, err := mime.Parse(strings.NewReader(raw), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)

	assert.Equal(t, "reply@example.com", parsed.MessageID)
	assert.Equal(t, "original@example.com", parsed.InReplyTo)
	assert.Equal(t, []string{"first@example.com", "original@example.com"}, parsed.References)
}

func TestMIMEParseToEmail_MultipartAlternative(t *testing.T) {
	t.Parallel()

	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Multipart\r\n" +
		"Message-Id: <multi@example.com>\r\n" +
		"Date: Wed, 03 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: multipart/alternative; boundary=testbound\r\n" +
		"\r\n" +
		"--testbound\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Plain text version\r\n" +
		"--testbound\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>HTML version</p>\r\n" +
		"--testbound--\r\n"

	parsed, err := mime.Parse(strings.NewReader(raw), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)

	assert.Contains(t, parsed.TextBody, "Plain text version")
	assert.Contains(t, parsed.HTMLBody, "<p>HTML version</p>")
}

func TestMIMEParseToEmail_EmptyBody(t *testing.T) {
	t.Parallel()

	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Empty\r\n" +
		"Message-Id: <empty@example.com>\r\n" +
		"Date: Thu, 04 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n"

	parsed, err := mime.Parse(strings.NewReader(raw), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)
	assert.Equal(t, "Empty", parsed.Subject)
	assert.Equal(t, "empty@example.com", parsed.MessageID)
}

func TestMIMEParseToEmail_CC(t *testing.T) {
	t.Parallel()

	raw := "From: sender@example.com\r\n" +
		"To: alice@example.com\r\n" +
		"Cc: bob@example.com, carol@example.com\r\n" +
		"Subject: With CC\r\n" +
		"Message-Id: <cc@example.com>\r\n" +
		"Date: Fri, 05 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Body\r\n"

	parsed, err := mime.Parse(strings.NewReader(raw), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)
	assert.Contains(t, parsed.CC, "bob@example.com")
	assert.Contains(t, parsed.CC, "carol@example.com")
}

// --- Compose MIME Threading Pipeline Tests ---

func TestComposeMIMEThreading_InReplyTo(t *testing.T) {
	t.Parallel()

	req := &send.ComposeRequest{
		FromAddress:        "me@example.com",
		ToAddresses:        []string{"you@example.com"},
		Subject:            "Re: Test",
		HTMLBody:           "<p>Reply</p>",
		InReplyToMessageID: "abc@example.com",
		References:         "<prev@example.com>",
	}
	msg, err := send.ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	assert.Contains(t, body, "In-Reply-To: <abc@example.com>")
	assert.Contains(t, body, "References: <prev@example.com> <abc@example.com>")
}

func TestComposeMIMEThreading_NoReplyHeaders(t *testing.T) {
	t.Parallel()

	req := &send.ComposeRequest{
		FromAddress: "me@example.com",
		ToAddresses: []string{"you@example.com"},
		Subject:     "Fresh email",
		HTMLBody:    "<p>New</p>",
	}
	msg, err := send.ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	assert.NotContains(t, body, "In-Reply-To:")
	assert.NotContains(t, body, "References:")
}

func TestComposeMIMEThreading_ReferencesChain(t *testing.T) {
	t.Parallel()

	req := &send.ComposeRequest{
		FromAddress:        "me@example.com",
		ToAddresses:        []string{"you@example.com"},
		Subject:            "Re: Re: Topic",
		HTMLBody:           "<p>Deep reply</p>",
		InReplyToMessageID: "msg3@example.com",
		References:         "<msg1@example.com> <msg2@example.com>",
	}
	msg, err := send.ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	// References should include the full chain plus the reply-to message
	assert.Contains(t, body, "References: <msg1@example.com> <msg2@example.com> <msg3@example.com>")
	assert.Contains(t, body, "In-Reply-To: <msg3@example.com>")
}

// --- Parse-then-Normalize Pipeline Test ---

func TestParseAndNormalize_EndToEnd(t *testing.T) {
	t.Parallel()

	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Re: Fwd: Re: Important meeting notes\r\n" +
		"Message-Id: <chain@example.com>\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Meeting notes here\r\n"

	parsed, err := mime.Parse(strings.NewReader(raw), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)

	// The raw subject has nested prefixes.
	assert.Equal(t, "Re: Fwd: Re: Important meeting notes", parsed.Subject)

	// After normalization, all prefixes are stripped.
	normalized := NormalizeSubject(parsed.Subject)
	assert.Equal(t, "Important meeting notes", normalized)
}

// --- Extract Name Helper Tests ---

func TestExtractName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"email only", "alice@example.com", ""},
		{"name and email", "Alice Smith <alice@example.com>", "Alice Smith"},
		{"quoted name", "\"Bob Jones\" <bob@example.com>", "Bob Jones"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, extractName(tt.input))
		})
	}
}

// --- Compose Validation Tests ---

func TestComposeMIME_Validation(t *testing.T) {
	t.Parallel()

	t.Run("missing from", func(t *testing.T) {
		t.Parallel()
		req := &send.ComposeRequest{
			ToAddresses: []string{"bob@example.com"},
			Subject:     "No from",
			HTMLBody:    "<p>Test</p>",
		}
		_, err := send.ComposeMIME(req)
		assert.Error(t, err)
	})

	t.Run("missing to", func(t *testing.T) {
		t.Parallel()
		req := &send.ComposeRequest{
			FromAddress: "alice@example.com",
			Subject:     "No to",
			HTMLBody:    "<p>Test</p>",
		}
		_, err := send.ComposeMIME(req)
		assert.Error(t, err)
	})
}
