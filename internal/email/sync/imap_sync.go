package sync

import (
	"bytes"
	"fmt"
	"log/slog"
	"sort"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	emailpkg "github.com/darshan-kheni/regent/internal/email"
	imappkg "github.com/darshan-kheni/regent/internal/email/imap"
	"github.com/darshan-kheni/regent/internal/email/mime"
	"github.com/darshan-kheni/regent/internal/models"
)

// imapBatchSize is the number of UIDs fetched per IMAP FETCH batch.
const imapBatchSize = 50

// IMAPSyncer performs initial and incremental email sync over IMAP.
type IMAPSyncer struct {
	pool     *pgxpool.Pool
	thread   *emailpkg.ThreadService
	dedup    *emailpkg.DedupService
	storage  mime.StorageUploader
	progress *ProgressTracker
}

// NewIMAPSyncer creates a new IMAPSyncer.
func NewIMAPSyncer(pool *pgxpool.Pool, thread *emailpkg.ThreadService, dedup *emailpkg.DedupService, storage mime.StorageUploader, progress *ProgressTracker) *IMAPSyncer {
	return &IMAPSyncer{
		pool:     pool,
		thread:   thread,
		dedup:    dedup,
		storage:  storage,
		progress: progress,
	}
}

// Sync performs an IMAP sync for the given account.
// Steps: SELECT INBOX -> SEARCH SINCE 30 days -> filter above last_uid -> batch FETCH 50 ->
// MIME parse each -> assign thread -> dedup store -> update progress.
func (s *IMAPSyncer) Sync(ctx database.TenantContext, account *models.UserAccount, client *imapclient.Client, cursorID uuid.UUID) error {
	// Select INBOX.
	_, err := imappkg.SelectMailbox(client, "INBOX")
	if err != nil {
		return fmt.Errorf("selecting INBOX: %w", err)
	}

	// Search for messages from the last 30 days.
	since := sinceDate()
	allUIDs, err := imappkg.SearchSince(client, since)
	if err != nil {
		return fmt.Errorf("searching since %s: %w", since.Format("2006-01-02"), err)
	}

	if len(allUIDs) == 0 {
		slog.Info("no messages found for IMAP sync",
			"account_id", account.ID,
			"since", since.Format("2006-01-02"),
		)
		if err := s.progress.MarkCompleted(ctx, cursorID); err != nil {
			return fmt.Errorf("marking completed: %w", err)
		}
		return nil
	}

	// Get current cursor to check last_uid for resume.
	cursor, err := s.progress.GetOrCreateCursor(ctx, account.ID, "imap")
	if err != nil {
		return fmt.Errorf("getting cursor: %w", err)
	}

	// Filter UIDs above last_uid for resume support.
	uids := FilterAboveUID(allUIDs, cursor.LastUID)

	// Sort UIDs ascending so we process oldest first.
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })

	totalMessages := len(uids)
	if totalMessages == 0 {
		slog.Info("all messages already synced",
			"account_id", account.ID,
			"total_searched", len(allUIDs),
		)
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

	// Process in batches of 50.
	for i := 0; i < len(uids); i += imapBatchSize {
		// Check for context cancellation.
		select {
		case <-ctx.Done():
			slog.Warn("IMAP sync cancelled",
				"account_id", account.ID,
				"synced", syncedCount,
				"total", totalMessages,
			)
			return ctx.Err()
		default:
		}

		end := min(i+imapBatchSize, len(uids))
		batch := uids[i:end]

		// Fetch messages for this batch.
		fetched, err := imappkg.FetchMessages(client, batch)
		if err != nil {
			slog.Error("fetching IMAP batch",
				"account_id", account.ID,
				"batch_start", i,
				"error", err,
			)
			// Continue with partial results if we got some.
			if len(fetched) == 0 {
				continue
			}
		}

		// Process each fetched message.
		var maxUID imap.UID
		for _, msg := range fetched {
			if err := s.processIMAPMessage(ctx, account, msg); err != nil {
				slog.Warn("processing IMAP message",
					"account_id", account.ID,
					"uid", msg.UID,
					"error", err,
				)
				continue
			}
			syncedCount++
			if msg.UID > maxUID {
				maxUID = msg.UID
			}
		}

		// Update last_uid to the highest UID in this batch.
		if maxUID > 0 {
			if err := s.progress.UpdateLastUID(ctx, cursorID, int64(maxUID)); err != nil {
				slog.Error("updating last_uid", "error", err)
			}
		}

		// Update progress percentage.
		pct := (syncedCount * 100) / totalMessages
		if pct > 100 {
			pct = 100
		}
		if err := s.progress.UpdateProgress(ctx, cursorID, "syncing", pct, syncedCount); err != nil {
			slog.Error("updating sync progress", "error", err)
		}
	}

	// Mark sync as completed.
	if err := s.progress.MarkCompleted(ctx, cursorID); err != nil {
		return fmt.Errorf("marking completed: %w", err)
	}

	slog.Info("IMAP sync completed",
		"account_id", account.ID,
		"synced", syncedCount,
		"total", totalMessages,
	)

	return nil
}

// processIMAPMessage parses a single fetched IMAP message, assigns a thread, and stores it.
func (s *IMAPSyncer) processIMAPMessage(ctx database.TenantContext, account *models.UserAccount, msg *imappkg.FetchedMessage) error {
	if msg.RawBody == nil {
		return fmt.Errorf("empty raw body for UID %d", msg.UID)
	}

	emailID := uuid.New()

	// Parse the raw MIME message.
	parsed, err := mime.Parse(bytes.NewReader(msg.RawBody), emailID, ctx.TenantID, ctx.UserID, s.storage)
	if err != nil {
		return fmt.Errorf("parsing MIME for UID %d: %w", msg.UID, err)
	}

	// Set RawSize from the fetched body length.
	parsed.RawSize = int64(len(msg.RawBody))

	// Skip messages without a Message-ID (malformed).
	if parsed.MessageID == "" {
		slog.Warn("skipping message without Message-ID", "uid", msg.UID)
		return nil
	}

	// Assign thread.
	threadID, err := s.thread.AssignThread(ctx, parsed, account.ID)
	if err != nil {
		return fmt.Errorf("assigning thread for UID %d: %w", msg.UID, err)
	}

	// Store via dedup service (ON CONFLICT DO NOTHING).
	inserted, err := s.dedup.StoreEmail(ctx, parsed, account.ID, int64(msg.UID), "INBOX", "inbound", threadID)
	if err != nil {
		return fmt.Errorf("storing email UID %d: %w", msg.UID, err)
	}

	if !inserted {
		slog.Debug("duplicate email skipped",
			"uid", msg.UID,
			"message_id", parsed.MessageID,
		)
	}

	return nil
}

// FilterAboveUID filters IMAP UIDs to only include those strictly greater than lastUID.
// If lastUID is nil, all UIDs are returned (initial sync).
func FilterAboveUID(uids []imap.UID, lastUID *int64) []imap.UID {
	if lastUID == nil {
		return uids
	}
	threshold := imap.UID(*lastUID)
	var filtered []imap.UID
	for _, uid := range uids {
		if uid > threshold {
			filtered = append(filtered, uid)
		}
	}
	return filtered
}
