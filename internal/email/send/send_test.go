package send

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeMIME_BasicHeaders(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress: "alice@example.com",
		ToAddresses: []string{"bob@example.com"},
		Subject:     "Hello World",
		HTMLBody:    "<p>Hi Bob</p>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	assert.Contains(t, body, "From: alice@example.com\r\n")
	assert.Contains(t, body, "To: bob@example.com\r\n")
	assert.Contains(t, body, "Subject: Hello World\r\n")
	assert.Contains(t, body, "MIME-Version: 1.0\r\n")
	assert.Contains(t, body, "Content-Type: text/html; charset=utf-8\r\n")
	assert.Contains(t, body, "<p>Hi Bob</p>")
}

func TestComposeMIME_MultipleRecipients(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress: "alice@example.com",
		ToAddresses: []string{"bob@example.com", "carol@example.com"},
		CCAddresses: []string{"dave@example.com"},
		Subject:     "Team Update",
		HTMLBody:    "<p>Update</p>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	assert.Contains(t, body, "To: bob@example.com, carol@example.com\r\n")
	assert.Contains(t, body, "Cc: dave@example.com\r\n")
}

func TestComposeMIME_InReplyTo(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress:        "alice@example.com",
		ToAddresses:        []string{"bob@example.com"},
		Subject:            "Re: Hello",
		HTMLBody:           "<p>Reply</p>",
		InReplyToMessageID: "abc123@mail.example.com",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	// Must have angle brackets around message ID
	assert.Contains(t, body, "In-Reply-To: <abc123@mail.example.com>\r\n")
	assert.Contains(t, body, "References: <abc123@mail.example.com>\r\n")
}

func TestComposeMIME_InReplyTo_AlreadyBracketed(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress:        "alice@example.com",
		ToAddresses:        []string{"bob@example.com"},
		Subject:            "Re: Hello",
		HTMLBody:           "<p>Reply</p>",
		InReplyToMessageID: "<abc123@mail.example.com>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	// Should not double-bracket
	assert.Contains(t, body, "In-Reply-To: <abc123@mail.example.com>\r\n")
	assert.NotContains(t, body, "<<")
}

func TestComposeMIME_References(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress:        "alice@example.com",
		ToAddresses:        []string{"bob@example.com"},
		Subject:            "Re: Re: Hello",
		HTMLBody:           "<p>Reply chain</p>",
		InReplyToMessageID: "msg3@example.com",
		References:         "<msg1@example.com> <msg2@example.com>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	// References should contain the full chain plus the reply-to
	assert.Contains(t, body, "References: <msg1@example.com> <msg2@example.com> <msg3@example.com>\r\n")
	assert.Contains(t, body, "In-Reply-To: <msg3@example.com>\r\n")
}

func TestComposeMIME_NoReply(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress: "alice@example.com",
		ToAddresses: []string{"bob@example.com"},
		Subject:     "New Email",
		HTMLBody:    "<p>Fresh</p>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	assert.NotContains(t, body, "In-Reply-To:")
	assert.NotContains(t, body, "References:")
}

func TestComposeMIME_MissingFrom(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		ToAddresses: []string{"bob@example.com"},
		Subject:     "No From",
		HTMLBody:    "<p>Oops</p>",
	}

	_, err := ComposeMIME(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "from address")
}

func TestComposeMIME_MissingTo(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress: "alice@example.com",
		Subject:     "No To",
		HTMLBody:    "<p>Oops</p>",
	}

	_, err := ComposeMIME(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "recipient")
}

func TestXOAuth2Auth_Start(t *testing.T) {
	t.Parallel()

	auth := NewXOAuth2Auth("user@gmail.com", "ya29.token123")

	mechanism, blob, err := auth.Start(nil)
	require.NoError(t, err)

	assert.Equal(t, "XOAUTH2", mechanism)

	// Verify SASL XOAUTH2 blob format: "user=<email>\x01auth=Bearer <token>\x01\x01"
	expected := "user=user@gmail.com\x01auth=Bearer ya29.token123\x01\x01"
	assert.Equal(t, expected, string(blob))
}

func TestXOAuth2Auth_Next_NoMore(t *testing.T) {
	t.Parallel()

	auth := NewXOAuth2Auth("user@gmail.com", "token")
	resp, err := auth.Next(nil, false)
	assert.NoError(t, err)
	assert.Nil(t, resp)
}

func TestXOAuth2Auth_Next_Rejected(t *testing.T) {
	t.Parallel()

	auth := NewXOAuth2Auth("user@gmail.com", "token")
	_, err := auth.Next([]byte("error details"), true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "xoauth2 rejected")
}

func TestPlanRateLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		plan     string
		expected int
	}{
		{"free", 10},
		{"attache", 30},
		{"privy_council", 100},
		{"estate", 0},
	}

	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			limit, ok := PlanRateLimits[tt.plan]
			assert.True(t, ok, "plan %q should exist in PlanRateLimits", tt.plan)
			assert.Equal(t, tt.expected, limit)
		})
	}
}

func TestComposeMIME_NoCcWhenEmpty(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress: "alice@example.com",
		ToAddresses: []string{"bob@example.com"},
		Subject:     "No CC",
		HTMLBody:    "<p>Test</p>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	body := string(msg)
	assert.NotContains(t, body, "Cc:")
}

func TestComposeMIME_HeaderBodySeparation(t *testing.T) {
	t.Parallel()

	req := &ComposeRequest{
		FromAddress: "alice@example.com",
		ToAddresses: []string{"bob@example.com"},
		Subject:     "Test",
		HTMLBody:    "<p>Body here</p>",
	}

	msg, err := ComposeMIME(req)
	require.NoError(t, err)

	// Headers and body must be separated by \r\n\r\n
	parts := strings.SplitN(string(msg), "\r\n\r\n", 2)
	require.Len(t, parts, 2, "message should have header and body sections")
	assert.Equal(t, "<p>Body here</p>", parts[1])
}
