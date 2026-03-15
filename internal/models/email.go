package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Email represents a stored email in the database.
type Email struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	UserID         uuid.UUID       `json:"user_id"`
	AccountID      uuid.UUID       `json:"account_id"`
	MessageID      string          `json:"message_id"`
	ThreadID       *uuid.UUID      `json:"thread_id,omitempty"`
	UID            int64           `json:"uid"`
	Folder         string          `json:"folder"`
	Direction      string          `json:"direction"`
	FromAddress    string          `json:"from_address"`
	FromName       string          `json:"from_name,omitempty"`
	ToAddresses    json.RawMessage `json:"to_addresses"`
	CCAddresses    json.RawMessage `json:"cc_addresses"`
	Subject        string          `json:"subject,omitempty"`
	BodyText       string          `json:"body_text,omitempty"`
	BodyHTML       string          `json:"body_html,omitempty"`
	HasAttachments bool            `json:"has_attachments"`
	Attachments    json.RawMessage `json:"attachments"`
	Headers        json.RawMessage `json:"headers"`
	ReceivedAt     time.Time       `json:"received_at"`
	IsRead         bool            `json:"is_read"`
	IsStarred      bool            `json:"is_starred"`
	RawSize        int             `json:"raw_size,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	InReplyTo           *string         `json:"in_reply_to,omitempty"`
	ToneClassification  *string         `json:"tone_classification,omitempty"`
	ResponseTimeMinutes *float64        `json:"response_time_minutes,omitempty"`
}

// UserAccount represents a connected email account for a user.
type UserAccount struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	Provider     string     `json:"provider"`
	EmailAddress string     `json:"email_address"`
	DisplayName  string     `json:"display_name,omitempty"`
	IMAPHost     string     `json:"imap_host,omitempty"`
	IMAPPort     int        `json:"imap_port,omitempty"`
	SMTPHost     string     `json:"smtp_host,omitempty"`
	SMTPPort     int        `json:"smtp_port,omitempty"`
	SyncStatus   string     `json:"sync_status"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// DraftReply represents an AI-generated draft reply to an email.
type DraftReply struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	EmailID    uuid.UUID `json:"email_id"`
	Body       string    `json:"body"`
	Variant    string    `json:"variant"`
	ModelUsed  string    `json:"model_used,omitempty"`
	IsPremium  bool      `json:"is_premium"`
	Confidence float64   `json:"confidence,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}
