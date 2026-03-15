package observability

import (
	"log/slog"
	"os"
	"strings"
)

// redactedFields contains field names whose values should never appear in logs.
var redactedFields = map[string]bool{
	"password":      true,
	"token":         true,
	"secret":        true,
	"authorization": true,
	"cookie":        true,
	"api_key":       true,
	"credit_card":   true,
	"ssn":           true,
}

// NewLogger creates a structured slog.Logger based on the environment.
// Development: TextHandler with source, Debug level.
// Production: JSONHandler, Info level, PII redacted.
func NewLogger(environment string) *slog.Logger {
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if redactedFields[strings.ToLower(a.Key)] {
				a.Value = slog.StringValue("[REDACTED]")
			}
			return a
		},
	}

	if environment == "production" {
		opts.Level = slog.LevelInfo
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}

	opts.Level = slog.LevelDebug
	opts.AddSource = true
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
