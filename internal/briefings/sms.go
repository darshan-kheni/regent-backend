package briefings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

// TwilioSMSChannel sends SMS notifications via Twilio.
type TwilioSMSChannel struct {
	client      *twilio.RestClient
	fromNumber  string
	callbackURL string
	tracker     *DeliveryTracker
	available   bool
	lastError   string
	lastSent    time.Time
}

// NewTwilioSMSChannel creates a new Twilio SMS channel.
// Returns nil if credentials are not configured.
func NewTwilioSMSChannel(accountSID, authToken, fromNumber, callbackURL string, tracker *DeliveryTracker) *TwilioSMSChannel {
	if accountSID == "" || authToken == "" || fromNumber == "" {
		return nil
	}

	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: accountSID,
		Password: authToken,
	})

	return &TwilioSMSChannel{
		client:      client,
		fromNumber:  fromNumber,
		callbackURL: callbackURL,
		tracker:     tracker,
		available:   true,
	}
}

func (s *TwilioSMSChannel) Send(ctx context.Context, r Recipient, b Briefing) error {
	if r.Phone == "" {
		return fmt.Errorf("no phone number for user %s", r.UserID)
	}

	body := FormatSMS(b)

	params := &twilioApi.CreateMessageParams{}
	params.SetTo(r.Phone)
	params.SetFrom(s.fromNumber)
	params.SetBody(body)
	if s.callbackURL != "" {
		params.SetStatusCallback(s.callbackURL)
	}

	resp, err := s.client.Api.CreateMessage(params)
	if err != nil {
		s.lastError = err.Error()
		return fmt.Errorf("twilio send: %w", err)
	}

	s.lastSent = time.Now()
	s.lastError = ""

	// Track cost: ~$0.0079 per segment, round to 1 cent per segment
	segments := CountSMSSegments(body)
	externalID := ""
	if resp.Sid != nil {
		externalID = *resp.Sid
	}

	slog.Info("sms sent", "to", r.Phone, "segments", segments, "sid", externalID)

	if s.tracker != nil {
		s.tracker.LogSend(ctx, b, "sms", externalID, segments, "")
	}

	return nil
}

func (s *TwilioSMSChannel) ValidateConfig(cfg ChannelConfig) error {
	if cfg.Phone == "" {
		return fmt.Errorf("SMS phone number required")
	}
	if !strings.HasPrefix(cfg.Phone, "+") {
		return fmt.Errorf("phone must be E.164 format (start with +)")
	}
	return nil
}

func (s *TwilioSMSChannel) Status() ChannelStatus {
	return ChannelStatus{
		Available: s.available,
		LastError: s.lastError,
		LastSent:  s.lastSent,
	}
}

func (s *TwilioSMSChannel) Name() string { return "sms" }
