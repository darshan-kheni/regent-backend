package briefings

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/api/option"
)

// PushChannel sends push notifications via Firebase Cloud Messaging v1.
type PushChannel struct {
	client    *messaging.Client
	pool      *pgxpool.Pool
	tracker   *DeliveryTracker
	available bool
	lastError string
	lastSent  time.Time
}

// NewPushChannel creates a new FCM push channel.
// Returns nil if Firebase credentials are not configured.
func NewPushChannel(projectID, credentialsFile string, pool *pgxpool.Pool, tracker *DeliveryTracker) *PushChannel {
	if projectID == "" || credentialsFile == "" {
		return nil
	}

	ctx := context.Background()
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID},
		option.WithCredentialsFile(credentialsFile))
	if err != nil {
		slog.Error("push channel: firebase init failed", "error", err)
		return nil
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		slog.Error("push channel: fcm client init failed", "error", err)
		return nil
	}

	return &PushChannel{
		client:    client,
		pool:      pool,
		tracker:   tracker,
		available: true,
	}
}

func (p *PushChannel) Send(ctx context.Context, r Recipient, b Briefing) error {
	if len(r.DeviceTokens) == 0 {
		return nil // No devices registered
	}

	msg := &messaging.MulticastMessage{
		Tokens: r.DeviceTokens,
		Notification: &messaging.Notification{
			Title: fmt.Sprintf("From %s", b.SenderName),
			Body:  b.Subject,
		},
		Data: map[string]string{
			"email_id":   b.EmailID.String(),
			"priority":   strconv.Itoa(b.Priority),
			"category":   b.Category,
			"action_url": b.ActionURL,
		},
		Android: &messaging.AndroidConfig{
			Priority: priorityToAndroid(b.Priority),
			Notification: &messaging.AndroidNotification{
				ChannelID: channelForPriority(b.Priority),
				Sound:     soundForPriority(b.Priority),
			},
		},
		APNS: &messaging.APNSConfig{
			Headers: map[string]string{
				"apns-priority": apnsPriority(b.Priority),
			},
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:    soundForPriority(b.Priority),
					Category: "REGENT_EMAIL",
				},
			},
		},
	}

	// Use SendEachForMulticast (NOT deprecated SendMulticast)
	resp, err := p.client.SendEachForMulticast(ctx, msg)
	if err != nil {
		p.lastError = err.Error()
		return fmt.Errorf("fcm send: %w", err)
	}

	p.lastSent = time.Now()
	p.lastError = ""

	// Check individual results for stale tokens
	for i, result := range resp.Responses {
		if result.Error != nil {
			if messaging.IsRegistrationTokenNotRegistered(result.Error) {
				slog.Info("push: removing stale device token", "token_prefix", r.DeviceTokens[i][:8])
				p.deleteDeviceToken(ctx, r.DeviceTokens[i])
			}
		}
	}

	slog.Info("push sent", "user_id", b.UserID, "devices", len(r.DeviceTokens),
		"success", resp.SuccessCount, "failure", resp.FailureCount)

	if p.tracker != nil {
		p.tracker.LogSend(ctx, b, "push", "", 0, "") // FCM is free
	}

	return nil
}

func (p *PushChannel) ValidateConfig(_ ChannelConfig) error { return nil }

func (p *PushChannel) Status() ChannelStatus {
	return ChannelStatus{
		Available: p.available,
		LastError: p.lastError,
		LastSent:  p.lastSent,
	}
}

func (p *PushChannel) Name() string { return "push" }

// deleteDeviceToken removes a stale token from the database.
func (p *PushChannel) deleteDeviceToken(ctx context.Context, token string) {
	if p.pool == nil {
		return
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
}

// priorityToAndroid maps email priority to FCM Android priority.
func priorityToAndroid(priority int) string {
	if priority > 60 {
		return "high"
	}
	return "normal"
}

// apnsPriority maps email priority to APNs priority header.
func apnsPriority(priority int) string {
	if priority > 60 {
		return "10"
	}
	return "5"
}

// soundForPriority selects notification sound based on priority.
func soundForPriority(priority int) string {
	if priority > 80 {
		return "critical"
	}
	if priority > 60 {
		return "default"
	}
	return ""
}

// channelForPriority selects the Android notification channel.
func channelForPriority(priority int) string {
	if priority > 80 {
		return "regent_critical"
	}
	if priority > 60 {
		return "regent_high"
	}
	return "regent_normal"
}
