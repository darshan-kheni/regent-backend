package briefings

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Channel is the interface all notification channels must implement.
type Channel interface {
	// Send delivers a briefing to a recipient via this channel.
	Send(ctx context.Context, recipient Recipient, briefing Briefing) error
	// ValidateConfig checks whether the channel's configuration is valid.
	ValidateConfig(cfg ChannelConfig) error
	// Status returns the current availability status of this channel.
	Status() ChannelStatus
	// Name returns the channel identifier (sms, whatsapp, signal, push, email_digest).
	Name() string
}

// Briefing represents a notification event derived from an email's AI categorization.
type Briefing struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TenantID   uuid.UUID
	EmailID    uuid.UUID
	Priority   int // 0-100
	Category   string
	SenderName string
	Subject    string
	Summary    string
	ActionURL  string
	CreatedAt  time.Time
}

// Recipient holds all possible delivery addresses for a user across channels.
type Recipient struct {
	UserID       uuid.UUID
	Phone        string   // E.164 format for SMS
	Email        string   // For digest
	DeviceTokens []string // FCM tokens
	SignalID     string   // Signal phone number
	WhatsAppID   string   // WhatsApp phone number
}

// ChannelConfig holds per-channel configuration for validation.
type ChannelConfig struct {
	Enabled bool
	Phone   string
}

// ChannelStatus reports a channel's current health.
type ChannelStatus struct {
	Available bool
	LastError string
	LastSent  time.Time
}
