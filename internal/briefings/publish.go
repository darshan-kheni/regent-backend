package briefings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// NotificationEvent represents an event to be published to the notification
// stream for the BriefingDispatcher to consume and route to channels.
type NotificationEvent struct {
	UserID   string   `json:"user_id"`
	TenantID string   `json:"tenant_id"`
	Priority int      `json:"priority"`
	Category string   `json:"category"`
	Subject  string   `json:"subject"`
	Summary  string   `json:"summary"`
	Channels []string `json:"channels"` // e.g. ["email", "push", "sms"]
}

// NotificationStream is the Redis stream name for notification events.
const NotificationStream = "notification_events"

// PublishNotificationEvent publishes a notification event to the Redis Stream
// for the BriefingDispatcher to consume. This is the public API for any
// subsystem (billing, AI pipeline, etc.) to trigger notifications.
func PublishNotificationEvent(ctx context.Context, rdb *redis.Client, event NotificationEvent) error {
	if rdb == nil {
		slog.Warn("briefings: Redis client is nil, skipping notification publish",
			"user_id", event.UserID,
			"category", event.Category,
		)
		return nil
	}

	channelsJSON, err := json.Marshal(event.Channels)
	if err != nil {
		return fmt.Errorf("marshaling channels: %w", err)
	}

	err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: NotificationStream,
		MaxLen: 10000,
		Approx: true,
		Values: map[string]interface{}{
			"user_id":   event.UserID,
			"tenant_id": event.TenantID,
			"priority":  strconv.Itoa(event.Priority),
			"category":  event.Category,
			"subject":   event.Subject,
			"summary":   event.Summary,
			"channels":  string(channelsJSON),
		},
	}).Err()
	if err != nil {
		return fmt.Errorf("publishing notification event: %w", err)
	}

	slog.Debug("briefings: published notification event",
		"user_id", event.UserID,
		"category", event.Category,
		"priority", event.Priority,
		"channels", strings.Join(event.Channels, ","),
	)
	return nil
}
