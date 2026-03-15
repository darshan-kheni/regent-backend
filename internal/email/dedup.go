package email

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/email/mime"
)

// DedupService handles email deduplication via message_id lookups
// and ON CONFLICT DO NOTHING inserts.
type DedupService struct {
	pool *pgxpool.Pool
}

// NewDedupService creates a new DedupService.
func NewDedupService(pool *pgxpool.Pool) *DedupService {
	return &DedupService{pool: pool}
}

// Exists checks whether an email with the given message_id already exists
// for the specified account.
func (ds *DedupService) Exists(ctx database.TenantContext, accountID uuid.UUID, messageID string) (bool, error) {
	conn, err := ds.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return false, fmt.Errorf("setting tenant context: %w", err)
	}

	var exists bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM emails WHERE account_id = $1 AND message_id = $2)`,
		accountID, messageID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking email existence: %w", err)
	}
	return exists, nil
}

// StoreEmail inserts an email into the database using ON CONFLICT (account_id, uid) DO NOTHING.
// Returns true if the email was inserted, false if it was a duplicate (already existed).
func (ds *DedupService) StoreEmail(
	ctx database.TenantContext,
	parsed *mime.ParsedEmail,
	accountID uuid.UUID,
	uid int64,
	folder string,
	direction string,
	threadID uuid.UUID,
) (bool, error) {
	conn, err := ds.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return false, fmt.Errorf("setting tenant context: %w", err)
	}

	toJSON, err := json.Marshal(parsed.To)
	if err != nil {
		return false, fmt.Errorf("marshaling to_addresses: %w", err)
	}
	ccJSON, err := json.Marshal(parsed.CC)
	if err != nil {
		return false, fmt.Errorf("marshaling cc_addresses: %w", err)
	}
	attachJSON, err := json.Marshal(parsed.Attachments)
	if err != nil {
		return false, fmt.Errorf("marshaling attachments: %w", err)
	}

	hasAttachments := len(parsed.Attachments) > 0

	tag, err := conn.Exec(ctx,
		`INSERT INTO emails (
			tenant_id, user_id, account_id, message_id, thread_id,
			uid, folder, direction, from_address, from_name,
			to_addresses, cc_addresses, subject, body_text, body_html,
			has_attachments, attachments, received_at, raw_size
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17, $18, $19
		) ON CONFLICT (account_id, uid) DO NOTHING`,
		ctx.TenantID, ctx.UserID, accountID, parsed.MessageID, threadID,
		uid, folder, direction, extractAddress(parsed.From), extractName(parsed.From),
		toJSON, ccJSON, parsed.Subject, parsed.TextBody, parsed.HTMLBody,
		hasAttachments, attachJSON, parsed.ReceivedAt, parsed.RawSize,
	)
	if err != nil {
		return false, fmt.Errorf("inserting email: %w", err)
	}

	// RowsAffected() returns 0 when ON CONFLICT DO NOTHING skips the insert.
	return tag.RowsAffected() > 0, nil
}

// StoreEmailBatch inserts multiple emails in a single batch operation.
// Returns the number of emails actually inserted (duplicates are skipped).
func (ds *DedupService) StoreEmailBatch(
	ctx database.TenantContext,
	emails []StoreEmailInput,
) (int64, error) {
	if len(emails) == 0 {
		return 0, nil
	}

	conn, err := ds.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return 0, fmt.Errorf("setting tenant context: %w", err)
	}

	batch := &pgx.Batch{}
	for _, e := range emails {
		toJSON, err := json.Marshal(e.Parsed.To)
		if err != nil {
			return 0, fmt.Errorf("marshaling to_addresses: %w", err)
		}
		ccJSON, err := json.Marshal(e.Parsed.CC)
		if err != nil {
			return 0, fmt.Errorf("marshaling cc_addresses: %w", err)
		}
		attachJSON, err := json.Marshal(e.Parsed.Attachments)
		if err != nil {
			return 0, fmt.Errorf("marshaling attachments: %w", err)
		}
		hasAttachments := len(e.Parsed.Attachments) > 0

		batch.Queue(
			`INSERT INTO emails (
				tenant_id, user_id, account_id, message_id, thread_id,
				uid, folder, direction, from_address, from_name,
				to_addresses, cc_addresses, subject, body_text, body_html,
				has_attachments, attachments, received_at, raw_size
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15,
				$16, $17, $18, $19
			) ON CONFLICT (account_id, uid) DO NOTHING`,
			ctx.TenantID, ctx.UserID, e.AccountID, e.Parsed.MessageID, e.ThreadID,
			e.UID, e.Folder, e.Direction, extractAddress(e.Parsed.From), extractName(e.Parsed.From),
			toJSON, ccJSON, e.Parsed.Subject, e.Parsed.TextBody, e.Parsed.HTMLBody,
			hasAttachments, attachJSON, e.Parsed.ReceivedAt, e.Parsed.RawSize,
		)
	}

	results := conn.SendBatch(ctx, batch)
	defer results.Close()

	var inserted int64
	for range emails {
		tag, err := results.Exec()
		if err != nil {
			return inserted, fmt.Errorf("executing batch insert: %w", err)
		}
		inserted += tag.RowsAffected()
	}
	return inserted, nil
}

// StoreEmailInput groups the parameters needed to store a single email.
type StoreEmailInput struct {
	Parsed    *mime.ParsedEmail
	AccountID uuid.UUID
	UID       int64
	Folder    string
	Direction string
	ThreadID  uuid.UUID
}

// extractName extracts a display name from a "Name <email>" formatted address.
// Returns empty string if no name is present.
func extractName(from string) string {
	if idx := len(from) - 1; idx > 0 && from[idx] == '>' {
		for i := idx - 1; i >= 0; i-- {
			if from[i] == '<' {
				name := from[:i]
				name = trimQuotes(name)
				return name
			}
		}
	}
	return ""
}

// extractAddress extracts just the email address from a "Name <email>" or plain "email" string.
func extractAddress(from string) string {
	if idx := len(from) - 1; idx > 0 && from[idx] == '>' {
		for i := idx - 1; i >= 0; i-- {
			if from[i] == '<' {
				return from[i+1 : idx]
			}
		}
	}
	return from
}

// trimQuotes removes surrounding quotes and whitespace from a string.
func trimQuotes(s string) string {
	s = fmt.Sprintf("%s", s) // force copy
	for len(s) > 0 && (s[0] == ' ' || s[0] == '"') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '"') {
		s = s[:len(s)-1]
	}
	return s
}
