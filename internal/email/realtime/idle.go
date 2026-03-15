package realtime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

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

// idleRestartInterval is the maximum time to stay in IDLE before restarting.
// RFC 2177 recommends < 30 minutes; we use 28 minutes for safety margin.
const idleRestartInterval = 28 * time.Minute

// AIEnqueuer abstracts AI job enqueueing for the realtime layer.
type AIEnqueuer interface {
	EnqueueEmail(ctx context.Context, emailID, userID, tenantID uuid.UUID, plan string) error
}

// IDLEWatcher maintains a persistent IMAP IDLE connection for real-time
// email detection. When the server sends an EXISTS notification (new mail),
// IDLE is broken and new messages are fetched, parsed, threaded, deduped,
// and stored.
type IDLEWatcher struct {
	pool        *pgxpool.Pool
	threading   *emailpkg.ThreadService
	dedup       *emailpkg.DedupService
	storage     mime.StorageUploader
	account     *models.UserAccount
	lastUID     imap.UID
	aiEnqueuer  AIEnqueuer
	aiPlan      string
}

// NewIDLEWatcher creates a new IDLEWatcher for the given email account.
func NewIDLEWatcher(
	pool *pgxpool.Pool,
	threading *emailpkg.ThreadService,
	dedup *emailpkg.DedupService,
	storage mime.StorageUploader,
	account *models.UserAccount,
) *IDLEWatcher {
	return &IDLEWatcher{
		pool:      pool,
		threading: threading,
		dedup:     dedup,
		storage:   storage,
		account:   account,
	}
}

// SetLastUID sets the starting UID watermark. Messages with UIDs at or below
// this value are considered already synced and will be skipped.
func (w *IDLEWatcher) SetLastUID(uid imap.UID) {
	w.lastUID = uid
}

// SetAIEnqueuer configures the AI processing enqueuer and the user's billing plan.
// When set, newly stored emails will be automatically enqueued for AI processing.
func (w *IDLEWatcher) SetAIEnqueuer(enqueuer AIEnqueuer, plan string) {
	w.aiEnqueuer = enqueuer
	w.aiPlan = plan
}

// RunIDLE enters the IDLE loop: IDLE → wait for EXISTS → break IDLE → FETCH
// new messages → re-enter IDLE. The loop auto-restarts every 28 minutes per
// RFC 2177. The newMail channel receives signals from the UnilateralDataHandler
// when the server sends EXISTS notifications.
//
// The caller must set up the imapclient.Options.UnilateralDataHandler.Mailbox
// callback to send on the newMail channel when NumMessages increases.
func (w *IDLEWatcher) RunIDLE(ctx context.Context, client *imapclient.Client, newMail <-chan struct{}) error {
	if _, err := imappkg.SelectMailbox(client, "INBOX"); err != nil {
		return fmt.Errorf("SELECT INBOX: %w", err)
	}

	slog.Info("IDLE watcher started",
		"account_id", w.account.ID,
		"email", w.account.EmailAddress,
		"last_uid", w.lastUID,
	)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		idleCmd, err := client.Idle()
		if err != nil {
			return fmt.Errorf("IDLE start: %w", err)
		}

		timer := time.NewTimer(idleRestartInterval)

		select {
		case <-ctx.Done():
			timer.Stop()
			_ = idleCmd.Close()
			return ctx.Err()

		case <-newMail:
			timer.Stop()
			if err := idleCmd.Close(); err != nil {
				return fmt.Errorf("IDLE close after EXISTS: %w", err)
			}
			if err := idleCmd.Wait(); err != nil {
				slog.Warn("IDLE wait after EXISTS",
					"account_id", w.account.ID,
					"error", err,
				)
			}
			w.fetchNewEmails(ctx, client)

		case <-timer.C:
			// RFC 2177: restart IDLE before the 29-minute server timeout.
			if err := idleCmd.Close(); err != nil {
				return fmt.Errorf("IDLE close on timer: %w", err)
			}
			if err := idleCmd.Wait(); err != nil {
				slog.Warn("IDLE wait on timer restart",
					"account_id", w.account.ID,
					"error", err,
				)
			}
			slog.Debug("IDLE restarted (28-min timer)",
				"account_id", w.account.ID,
			)
		}
	}
}

// fetchNewEmails searches for UIDs above w.lastUID and processes them through
// the parse → thread → dedup → store pipeline.
func (w *IDLEWatcher) fetchNewEmails(ctx context.Context, client *imapclient.Client) {
	// Search for recent messages (last 1 day as a safety window).
	since := time.Now().AddDate(0, 0, -1)
	uids, err := imappkg.SearchSince(client, since)
	if err != nil {
		slog.Error("search after IDLE notification",
			"account_id", w.account.ID,
			"error", err,
		)
		return
	}

	// Filter to only new UIDs above the watermark.
	var newUIDs []imap.UID
	for _, uid := range uids {
		if uid > w.lastUID {
			newUIDs = append(newUIDs, uid)
		}
	}
	if len(newUIDs) == 0 {
		return
	}

	slog.Info("fetching new emails after IDLE",
		"account_id", w.account.ID,
		"count", len(newUIDs),
	)

	messages, err := imappkg.FetchMessages(client, newUIDs)
	if err != nil {
		slog.Error("fetch after IDLE notification",
			"account_id", w.account.ID,
			"error", err,
		)
		// Process any partial results we did get.
		if len(messages) == 0 {
			return
		}
	}

	tCtx := database.WithTenant(ctx, w.account.TenantID, w.account.UserID)
	for _, msg := range messages {
		if err := w.processMessage(tCtx, msg); err != nil {
			slog.Warn("processing realtime message",
				"account_id", w.account.ID,
				"uid", msg.UID,
				"error", err,
			)
			continue
		}
		if msg.UID > w.lastUID {
			w.lastUID = msg.UID
		}
	}
}

// processMessage runs a single fetched message through the full pipeline:
// MIME parse → thread assignment → dedup store.
func (w *IDLEWatcher) processMessage(ctx database.TenantContext, msg *imappkg.FetchedMessage) error {
	if msg.RawBody == nil {
		return fmt.Errorf("empty raw body for UID %d", msg.UID)
	}

	emailID := uuid.New()

	// 1. Parse the raw MIME message.
	parsed, err := mime.Parse(bytes.NewReader(msg.RawBody), emailID, ctx.TenantID, ctx.UserID, w.storage)
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

	// 2. Assign thread.
	threadID, err := w.threading.AssignThread(ctx, parsed, w.account.ID)
	if err != nil {
		return fmt.Errorf("assigning thread for UID %d: %w", msg.UID, err)
	}

	// 3. Store via dedup service (ON CONFLICT DO NOTHING).
	inserted, err := w.dedup.StoreEmail(ctx, parsed, w.account.ID, int64(msg.UID), "INBOX", "inbound", threadID)
	if err != nil {
		return fmt.Errorf("storing email UID %d: %w", msg.UID, err)
	}

	if inserted {
		slog.Info("new email stored via realtime",
			"uid", msg.UID,
			"message_id", parsed.MessageID,
			"subject", parsed.Subject,
		)
		// Enqueue for AI processing pipeline (categorize, summarize, draft replies).
		if w.aiEnqueuer != nil {
			if err := w.aiEnqueuer.EnqueueEmail(ctx, emailID, ctx.UserID, ctx.TenantID, w.aiPlan); err != nil {
				slog.Warn("failed to enqueue for AI processing",
					"email_id", emailID,
					"error", err,
				)
			}
		}
	} else {
		slog.Debug("duplicate email skipped in realtime",
			"uid", msg.UID,
			"message_id", parsed.MessageID,
		)
	}

	return nil
}

// Ensure IDLEWatcher and related types reference models to satisfy the compiler.
var _ = (*models.UserAccount)(nil)
