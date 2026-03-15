package mime

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStorage implements StorageUploader for testing.
type mockStorage struct {
	uploaded map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{uploaded: make(map[string][]byte)}
}

func (m *mockStorage) Upload(_ context.Context, bucket, key string, reader io.Reader, contentType string) (string, error) {
	data, _ := io.ReadAll(reader)
	m.uploaded[key] = data
	return key, nil
}

const simpleTextEmail = "From: sender@example.com\r\n" +
	"To: recipient@example.com\r\n" +
	"Subject: Test Subject\r\n" +
	"Message-Id: <msg-001@example.com>\r\n" +
	"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Hello, this is a test email.\r\n"

const multipartAlternativeEmail = "From: sender@example.com\r\n" +
	"To: recipient@example.com\r\n" +
	"Subject: Multipart Test\r\n" +
	"Message-Id: <msg-002@example.com>\r\n" +
	"In-Reply-To: <msg-001@example.com>\r\n" +
	"References: <msg-000@example.com> <msg-001@example.com>\r\n" +
	"Date: Tue, 02 Jan 2024 12:00:00 +0000\r\n" +
	"Content-Type: multipart/alternative; boundary=boundary1\r\n" +
	"\r\n" +
	"--boundary1\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Plain text body\r\n" +
	"--boundary1\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<p>HTML body</p>\r\n" +
	"--boundary1--\r\n"

func TestParse_SimpleText(t *testing.T) {
	result, err := Parse(strings.NewReader(simpleTextEmail), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)

	assert.Equal(t, "msg-001@example.com", result.MessageID)
	assert.Equal(t, "Test Subject", result.Subject)
	assert.Equal(t, "sender@example.com", result.From)
	assert.Contains(t, result.To, "recipient@example.com")
	assert.Contains(t, result.TextBody, "Hello, this is a test email.")
	assert.Empty(t, result.HTMLBody)
	assert.Empty(t, result.Attachments)
}

func TestParse_MultipartAlternative(t *testing.T) {
	result, err := Parse(strings.NewReader(multipartAlternativeEmail), uuid.New(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)

	assert.Equal(t, "msg-002@example.com", result.MessageID)
	assert.Equal(t, "msg-001@example.com", result.InReplyTo)
	assert.Equal(t, []string{"msg-000@example.com", "msg-001@example.com"}, result.References)
	assert.Contains(t, result.TextBody, "Plain text body")
	assert.Contains(t, result.HTMLBody, "<p>HTML body</p>")
}

func TestParse_MultipartMixed_WithAttachment(t *testing.T) {
	storage := newMockStorage()

	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: With Attachment\r\n" +
		"Message-Id: <msg-003@example.com>\r\n" +
		"Date: Wed, 03 Jan 2024 12:00:00 +0000\r\n" +
		"Content-Type: multipart/mixed; boundary=boundary2\r\n" +
		"\r\n" +
		"--boundary2\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"See attached.\r\n" +
		"--boundary2\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
		"\r\n" +
		"FAKE_PDF_CONTENT\r\n" +
		"--boundary2--\r\n"

	tenantID, userID, emailID := uuid.New(), uuid.New(), uuid.New()
	result, err := Parse(strings.NewReader(raw), emailID, tenantID, userID, storage)
	require.NoError(t, err)

	assert.Contains(t, result.TextBody, "See attached.")
	require.Len(t, result.Attachments, 1)
	assert.Equal(t, "report.pdf", result.Attachments[0].Filename)
	assert.Equal(t, "application/pdf", result.Attachments[0].ContentType)

	// Verify uploaded to storage
	expectedKey := tenantID.String() + "/" + userID.String() + "/" + emailID.String() + "/report.pdf"
	assert.Contains(t, storage.uploaded, expectedKey)
}

func TestDecodeCharset_UTF8Passthrough(t *testing.T) {
	input := "Hello, World!"
	assert.Equal(t, input, DecodeCharset(input, "utf-8"))
	assert.Equal(t, input, DecodeCharset(input, "UTF-8"))
	assert.Equal(t, input, DecodeCharset(input, ""))
	assert.Equal(t, input, DecodeCharset(input, "us-ascii"))
}

func TestDecodeCharset_ISO88591(t *testing.T) {
	// ISO-8859-1 byte 0xE9 = é
	input := string([]byte{0xE9})
	result := DecodeCharset(input, "iso-8859-1")
	assert.Equal(t, "é", result)
}

func TestDecodeCharset_Windows1252(t *testing.T) {
	// Windows-1252 specific: 0x93 = left double quotation mark "
	input := string([]byte{0x93})
	result := DecodeCharset(input, "windows-1252")
	assert.Equal(t, "\u201c", result)
}

func TestDecodeCharset_UnknownCharset(t *testing.T) {
	input := "raw data"
	result := DecodeCharset(input, "totally-fake-charset")
	assert.Equal(t, input, result, "unknown charset should return raw input")
}

func TestParseReferences(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"<msg-001@example.com>", []string{"msg-001@example.com"}},
		{"<msg-001@example.com> <msg-002@example.com>", []string{"msg-001@example.com", "msg-002@example.com"}},
	}

	for _, tt := range tests {
		result := parseReferences(tt.input)
		assert.Equal(t, tt.expected, result, "input: %q", tt.input)
	}
}

func TestStreamToStorage(t *testing.T) {
	storage := newMockStorage()
	body := bytes.NewReader([]byte("file content here"))

	tenantID, userID, emailID := uuid.New(), uuid.New(), uuid.New()
	key, size, err := StreamToStorage(body, storage, tenantID, userID, emailID, "test.txt", "text/plain")

	require.NoError(t, err)
	assert.Equal(t, int64(17), size)
	assert.Contains(t, key, "test.txt")
	assert.Contains(t, storage.uploaded, key)
}
