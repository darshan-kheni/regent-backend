package briefings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// SignalChannel sends notifications via signal-cli-rest-api.
// EXPERIMENTAL: Privy Council/Estate only. No delivery receipts.
// Graceful fallback when Signal API is unreachable.
type SignalChannel struct {
	apiURL     string
	fromNumber string
	httpClient *http.Client

	mu     sync.Mutex
	status ChannelStatus
}

// NewSignalChannel creates a Signal channel.
func NewSignalChannel(apiURL, fromNumber string) *SignalChannel {
	return &SignalChannel{
		apiURL:     apiURL,
		fromNumber: fromNumber,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		status:     ChannelStatus{Available: apiURL != "" && fromNumber != ""},
	}
}

func (s *SignalChannel) Name() string { return "signal" }

func (s *SignalChannel) Status() ChannelStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *SignalChannel) ValidateConfig(cfg ChannelConfig) error {
	if s.apiURL == "" {
		return fmt.Errorf("signal: API URL not configured")
	}
	if s.fromNumber == "" {
		return fmt.Errorf("signal: from number not configured")
	}
	return nil
}

func (s *SignalChannel) Send(ctx context.Context, recipient Recipient, briefing Briefing) error {
	if recipient.SignalID == "" {
		return fmt.Errorf("signal: recipient has no Signal ID")
	}

	// Format message with markdown (Signal supports **bold** and *italic*)
	msg := formatSignalMessage(briefing)

	payload := signalSendRequest{
		Message:    msg,
		Number:     s.fromNumber,
		Recipients: []string{recipient.SignalID},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("signal: marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/v2/send", s.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("signal: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.mu.Lock()
		s.status.Available = false
		s.status.LastError = err.Error()
		s.mu.Unlock()
		return fmt.Errorf("signal: API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		s.mu.Lock()
		s.status.LastError = string(respBody)
		s.mu.Unlock()
		return fmt.Errorf("signal: API error %d: %s", resp.StatusCode, string(respBody))
	}

	s.mu.Lock()
	s.status.Available = true
	s.status.LastSent = time.Now()
	s.status.LastError = ""
	s.mu.Unlock()

	// No delivery receipts from Signal — mark as sent immediately
	slog.Info("signal: message sent",
		"recipient", recipient.SignalID,
		"briefing_id", briefing.ID,
		"priority", briefing.Priority,
	)
	return nil
}

// signalSendRequest is the payload for signal-cli-rest-api v2/send endpoint.
type signalSendRequest struct {
	Message    string   `json:"message"`
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
}

// formatSignalMessage formats a briefing for Signal with markdown.
func formatSignalMessage(b Briefing) string {
	var priority string
	switch {
	case b.Priority > 80:
		priority = "URGENT"
	case b.Priority > 60:
		priority = "Important"
	default:
		priority = "FYI"
	}

	msg := fmt.Sprintf("*%s* — REGENT\n\n**%s**\nFrom: %s\n\n%s",
		priority, b.Subject, b.SenderName, b.Summary)

	if b.ActionURL != "" {
		msg += fmt.Sprintf("\n\n%s", b.ActionURL)
	}

	return msg
}
