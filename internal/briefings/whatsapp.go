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

// WhatsAppChannel sends notifications via Meta WhatsApp Business Cloud API.
// Uses raw net/http (no Go SDK available). Plan-gated: Attache+ only.
// ALL messages are template-based (users never message Regent first).
type WhatsAppChannel struct {
	accessToken   string
	phoneNumberID string
	httpClient    *http.Client
	tracker       *DeliveryTracker

	mu     sync.Mutex
	status ChannelStatus
}

// NewWhatsAppChannel creates a WhatsApp channel.
// Returns nil if credentials are not configured.
func NewWhatsAppChannel(accessToken, phoneNumberID string, tracker *DeliveryTracker) *WhatsAppChannel {
	if accessToken == "" || phoneNumberID == "" {
		return nil
	}

	return &WhatsAppChannel{
		accessToken:   accessToken,
		phoneNumberID: phoneNumberID,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		tracker:       tracker,
		status: ChannelStatus{
			Available: true,
		},
	}
}

func (w *WhatsAppChannel) Name() string { return "whatsapp" }

func (w *WhatsAppChannel) Status() ChannelStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *WhatsAppChannel) ValidateConfig(cfg ChannelConfig) error {
	if w.accessToken == "" {
		return fmt.Errorf("whatsapp: access token not configured")
	}
	if w.phoneNumberID == "" {
		return fmt.Errorf("whatsapp: phone number ID not configured")
	}
	return nil
}

// whatsAppSendResponse is the response from the WhatsApp Cloud API send endpoint.
type whatsAppSendResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

func (w *WhatsAppChannel) Send(ctx context.Context, recipient Recipient, briefing Briefing) error {
	if recipient.WhatsAppID == "" {
		return fmt.Errorf("whatsapp: recipient has no WhatsApp ID")
	}

	// Select template based on priority
	var payload map[string]interface{}
	if briefing.Priority > 80 {
		payload = buildUrgentBriefingTemplate(recipient.WhatsAppID, briefing)
	} else {
		payload = buildUrgentBriefingTemplate(recipient.WhatsAppID, briefing)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://graph.facebook.com/v21.0/%s/messages", w.phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("whatsapp: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.mu.Lock()
		w.status.LastError = err.Error()
		w.mu.Unlock()

		if w.tracker != nil {
			w.tracker.LogSend(ctx, briefing, "whatsapp", "", 0, err.Error())
		}
		return fmt.Errorf("whatsapp: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		errMsg := string(respBody)
		w.mu.Lock()
		w.status.LastError = errMsg
		w.mu.Unlock()

		if w.tracker != nil {
			w.tracker.LogSend(ctx, briefing, "whatsapp", "", 0, errMsg)
		}
		return fmt.Errorf("whatsapp: API error %d: %s", resp.StatusCode, errMsg)
	}

	// Extract message ID from response for delivery tracking
	var sendResp whatsAppSendResponse
	externalID := ""
	if err := json.Unmarshal(respBody, &sendResp); err == nil && len(sendResp.Messages) > 0 {
		externalID = sendResp.Messages[0].ID
	}

	w.mu.Lock()
	w.status.LastSent = time.Now()
	w.status.LastError = ""
	w.mu.Unlock()

	if w.tracker != nil {
		w.tracker.LogSend(ctx, briefing, "whatsapp", externalID, 0, "")
	}

	slog.Info("whatsapp: message sent",
		"recipient", recipient.WhatsAppID,
		"briefing_id", briefing.ID,
		"priority", briefing.Priority,
		"external_id", externalID,
	)
	return nil
}
