package observability

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLogger_Development_TextHandler(t *testing.T) {
	logger := NewLogger("development")
	assert.NotNil(t, logger)
}

func TestNewLogger_Production_JSONHandler(t *testing.T) {
	logger := NewLogger("production")
	assert.NotNil(t, logger)
}

func TestPIIRedaction(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if redactedFields[a.Key] {
				a.Value = slog.StringValue("[REDACTED]")
			}
			return a
		},
	}
	logger := slog.New(slog.NewTextHandler(&buf, opts))

	logger.Info("test", "password", "secret123", "token", "abc", "user", "safe-value")

	output := buf.String()
	assert.Contains(t, output, "[REDACTED]")
	assert.NotContains(t, output, "secret123")
	assert.NotContains(t, output, "abc")
	assert.Contains(t, output, "safe-value")
}
