package sync

import (
	"bytes"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/api/gmail/v1"

	"github.com/darshan-kheni/regent/internal/database"
	emailpkg "github.com/darshan-kheni/regent/internal/email"
	gmailpkg "github.com/darshan-kheni/regent/internal/email/gmail"
	"github.com/darshan-kheni/regent/internal/email/mime"
	"github.com/darshan-kheni/regent/internal/models"
)

// GmailSyncer performs initial and incremental email sync via the Gmail API.
type GmailSyncer struct {
	pool     *pgxpool.Pool
	thread   *emailpkg.ThreadService
	dedup    *emailpkg.DedupService
	storage  mime.StorageUploader
	progress *ProgressTracker
}

// NewGmailSyncer creates a new GmailSyncer.
func NewGmailSyncer(pool *pgxpool.Pool, thread *emailpkg.ThreadService, dedup *emailpkg.DedupService, storage mime.StorageUploader, progress *ProgressTracker) *GmailSyncer {
	return &GmailSyncer{
		pool:     pool,
		thread:   thread,
		dedup:    dedup,
		storage:  storage,
		progress: progress,
	}
}

// Sync performs a Gmail API sync for the given account.
// Steps: ListMessages with after: query (30 days) -> for each message ID:
// GetRawMessage -> MIME parse -> assign thread -> dedup store -> update progress.
func (s *GmailSyncer) Sync(ctx database.TenantContext, account *models.UserAccount, svc *gmail.Service, cursorID uuid.UUID) error {
	// Check if this is a brand new account (first sync).
	cursor, err := s.progress.GetOrCreateCursor(ctx, account.ID, "gmail")
	if err != nil {
		return fmt.Errorf("getting gmail cursor: %w", err)
	}

	// Build query for last 30 days.
	since := sinceDate()
	query := fmt.Sprintf("after:%s", since.Format("2006/01/02"))

	// List all message IDs matching the query.
	messageIDs, err := gmailpkg.ListMessages(ctx, svc, account.EmailAddress, query)
	if err != nil {
		return fmt.Errorf("listing Gmail messages: %w", err)
	}

	totalMessages := len(messageIDs)
	if totalMessages == 0 {
		slog.Info("no messages found for Gmail sync",
			"account_id", account.ID,
			"since", since.Format("2006/01/02"),
		)
		if err := s.progress.MarkCompleted(ctx, cursorID); err != nil {
			return fmt.Errorf("marking completed: %w", err)
		}
		return nil
	}

	// NEW ACCOUNT: If last_uid is nil (first sync ever), skip past emails.
	// Set the cursor to the current message count so only future emails are fetched.
	if cursor.LastUID == nil && totalMessages > 0 {
		slog.Info("new Gmail account: skipping past emails, setting baseline",
			"account_id", account.ID,
			"existing_emails", totalMessages,
		)
		if err := s.progress.UpdateLastUID(ctx, cursorID, int64(totalMessages)); err != nil {
			return fmt.Errorf("setting Gmail baseline: %w", err)
		}
		if err := s.progress.MarkCompleted(ctx, cursorID); err != nil {
			return fmt.Errorf("marking completed: %w", err)
		}
		return nil
	}

	// Mark sync as started.
	if err := s.progress.MarkStarted(ctx, cursorID, totalMessages); err != nil {
		return fmt.Errorf("marking started: %w", err)
	}

	syncedCount := 0

	for i, msgID := range messageIDs {
		// Check for context cancellation.
		select {
		case <-ctx.Done():
			slog.Warn("Gmail sync cancelled",
				"account_id", account.ID,
				"synced", syncedCount,
				"total", totalMessages,
			)
			return ctx.Err()
		default:
		}

		if err := s.processGmailMessage(ctx, account, svc, msgID); err != nil {
			slog.Warn("processing Gmail message",
				"account_id", account.ID,
				"gmail_id", msgID,
				"error", err,
			)
			continue
		}
		syncedCount++

		// Update progress after each message.
		pct := ((i + 1) * 100) / totalMessages
		if pct > 100 {
			pct = 100
		}
		if err := s.progress.UpdateProgress(ctx, cursorID, "syncing", pct, syncedCount); err != nil {
			slog.Error("updating Gmail sync progress", "error", err)
		}
	}

	// Mark sync as completed.
	if err := s.progress.MarkCompleted(ctx, cursorID); err != nil {
		return fmt.Errorf("marking completed: %w", err)
	}

	slog.Info("Gmail sync completed",
		"account_id", account.ID,
		"synced", syncedCount,
		"total", totalMessages,
	)

	return nil
}

// processGmailMessage fetches and processes a single Gmail message.
func (s *GmailSyncer) processGmailMessage(ctx database.TenantContext, account *models.UserAccount, svc *gmail.Service, gmailMsgID string) error {
	// Fetch the raw message.
	raw, err := gmailpkg.GetRawMessage(ctx, svc, account.EmailAddress, gmailMsgID)
	if err != nil {
		return fmt.Errorf("fetching raw message %s: %w", gmailMsgID, err)
	}

	emailID := uuid.New()

	// Parse the raw MIME message.
	parsed, err := mime.Parse(bytes.NewReader(raw), emailID, ctx.TenantID, ctx.UserID, s.storage)
	if err != nil {
		return fmt.Errorf("parsing MIME for %s: %w", gmailMsgID, err)
	}

	// Set RawSize from the fetched body length.
	parsed.RawSize = int64(len(raw))

	// Skip messages without a Message-ID.
	if parsed.MessageID == "" {
		slog.Warn("skipping Gmail message without Message-ID", "gmail_id", gmailMsgID)
		return nil
	}

	// Check for duplicate before threading (cheaper operation).
	exists, err := s.dedup.Exists(ctx, account.ID, parsed.MessageID)
	if err != nil {
		return fmt.Errorf("checking duplicate for %s: %w", gmailMsgID, err)
	}
	if exists {
		slog.Debug("duplicate Gmail message skipped",
			"gmail_id", gmailMsgID,
			"message_id", parsed.MessageID,
		)
		return nil
	}

	// Assign thread.
	threadID, err := s.thread.AssignThread(ctx, parsed, account.ID)
	if err != nil {
		return fmt.Errorf("assigning thread for %s: %w", gmailMsgID, err)
	}

	// Store via dedup service. Gmail doesn't have IMAP UIDs, so use 0.
	_, err = s.dedup.StoreEmail(ctx, parsed, account.ID, 0, "INBOX", "inbound", threadID)
	if err != nil {
		return fmt.Errorf("storing Gmail message %s: %w", gmailMsgID, err)
	}

	return nil
}
