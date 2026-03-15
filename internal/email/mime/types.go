package mime

import (
	"context"
	"io"
	"time"
)

// ParsedEmail holds the parsed result of a raw RFC 2822 email.
type ParsedEmail struct {
	MessageID    string
	InReplyTo    string
	References   []string
	Subject      string
	From         string
	To           []string
	CC           []string
	TextBody     string
	HTMLBody     string
	ReceivedAt   time.Time
	Attachments  []Attachment
	InlineImages []InlineImage
	RawSize      int64
}

// Attachment represents an extracted email attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	StorageKey  string
}

// InlineImage represents an inline image referenced by Content-ID.
type InlineImage struct {
	CID        string
	StorageKey string
}

// StorageUploader is the interface for uploading attachments.
// Implemented by Supabase Storage client. Easy to mock in tests.
type StorageUploader interface {
	Upload(ctx context.Context, bucket, key string, reader io.Reader, contentType string) (string, error)
}
