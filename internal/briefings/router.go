package briefings

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotificationRouter selects which channels to use based on briefing priority.
type NotificationRouter struct {
	channels map[string]Channel
	pool     *pgxpool.Pool
}

// UserNotificationPrefs holds per-user notification preferences from the database.
type UserNotificationPrefs struct {
	SMSEnabled      bool
	WhatsAppEnabled bool
	SignalEnabled   bool
	PushEnabled     bool
	DigestEnabled   bool
	PrimaryChannel  string
	QuietStart      *time.Time
	QuietEnd        *time.Time
	QuietTimezone   string
	VIPBreaksQuiet  bool
}

// NewNotificationRouter creates a router with the given channels.
func NewNotificationRouter(channels map[string]Channel, pool *pgxpool.Pool) *NotificationRouter {
	return &NotificationRouter{
		channels: channels,
		pool:     pool,
	}
}

// Route selects channels based on briefing priority and user preferences.
//   - Critical (>80): ALL active channels immediately
//   - High (60-80): primary channel only
//   - Normal (<=60): email_digest only
func (r *NotificationRouter) Route(ctx context.Context, briefing Briefing) []Channel {
	prefs, err := r.loadPrefs(ctx, briefing.UserID)
	if err != nil {
		slog.Warn("failed to load notification prefs, falling back to push",
			"user_id", briefing.UserID, "error", err)
		if ch, ok := r.channels["push"]; ok {
			return []Channel{ch}
		}
		return nil
	}

	switch {
	case briefing.Priority > 80:
		return r.allActiveChannels(prefs)
	case briefing.Priority > 60:
		if ch, ok := r.channels[prefs.PrimaryChannel]; ok {
			return []Channel{ch}
		}
		return nil
	default:
		if ch, ok := r.channels["email_digest"]; ok {
			return []Channel{ch}
		}
		return nil
	}
}

// allActiveChannels returns all channels the user has enabled.
func (r *NotificationRouter) allActiveChannels(prefs *UserNotificationPrefs) []Channel {
	var active []Channel
	type chanCheck struct {
		name    string
		enabled bool
	}
	checks := []chanCheck{
		{"sms", prefs.SMSEnabled},
		{"whatsapp", prefs.WhatsAppEnabled},
		{"signal", prefs.SignalEnabled},
		{"push", prefs.PushEnabled},
		{"email_digest", prefs.DigestEnabled},
	}
	for _, c := range checks {
		if c.enabled {
			if ch, ok := r.channels[c.name]; ok {
				active = append(active, ch)
			}
		}
	}
	return active
}

// loadPrefs loads user notification preferences from the database.
func (r *NotificationRouter) loadPrefs(ctx context.Context, userID uuid.UUID) (*UserNotificationPrefs, error) {
	prefs := &UserNotificationPrefs{
		PushEnabled:    true,
		DigestEnabled:  true,
		PrimaryChannel: "push",
		QuietTimezone:  "UTC",
	}

	if r.pool == nil {
		return prefs, nil
	}

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return prefs, err
	}
	defer conn.Release()

	err = conn.QueryRow(ctx,
		`SELECT sms_enabled, whatsapp_enabled, signal_enabled, push_enabled, digest_enabled,
		        primary_channel, quiet_start, quiet_end, quiet_timezone, vip_breaks_quiet
		 FROM user_notification_prefs WHERE user_id = $1`, userID,
	).Scan(
		&prefs.SMSEnabled, &prefs.WhatsAppEnabled, &prefs.SignalEnabled,
		&prefs.PushEnabled, &prefs.DigestEnabled, &prefs.PrimaryChannel,
		&prefs.QuietStart, &prefs.QuietEnd, &prefs.QuietTimezone, &prefs.VIPBreaksQuiet,
	)
	if err != nil {
		// No prefs row yet — return defaults
		return prefs, nil
	}

	return prefs, nil
}
