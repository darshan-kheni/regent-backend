package sync

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/api/gmail/v1"

	"github.com/darshan-kheni/regent/internal/database"
	emailpkg "github.com/darshan-kheni/regent/internal/email"
	"github.com/darshan-kheni/regent/internal/email/mime"
	"github.com/darshan-kheni/regent/internal/models"
)

// syncWindow is the number of days to look back on initial sync.
const syncWindow = 30

// SyncEngine orchestrates email sync, routing to the appropriate syncer
// based on the account provider (IMAP or Gmail).
type SyncEngine struct {
	pool      *pgxpool.Pool
	imapSync  *IMAPSyncer
	gmailSync *GmailSyncer
	progress  *ProgressTracker
}

// NewSyncEngine creates a new SyncEngine with all dependencies.
func NewSyncEngine(pool *pgxpool.Pool, thread *emailpkg.ThreadService, dedup *emailpkg.DedupService, storage mime.StorageUploader) *SyncEngine {
	progress := NewProgressTracker(pool)
	return &SyncEngine{
		pool:      pool,
		imapSync:  NewIMAPSyncer(pool, thread, dedup, storage, progress),
		gmailSync: NewGmailSyncer(pool, thread, dedup, storage, progress),
		progress:  progress,
	}
}

// Sync performs an email sync for the given account.
// Routes to IMAP or Gmail syncer based on account.Provider.
// Exactly one of imapClient or gmailSvc must be non-nil, matching the provider.
func (e *SyncEngine) Sync(ctx database.TenantContext, account *models.UserAccount, imapClient *imapclient.Client, gmailSvc *gmail.Service) error {
	slog.Info("starting email sync",
		"account_id", account.ID,
		"provider", account.Provider,
		"email", account.EmailAddress,
	)

	// Determine provider for cursor.
	provider := providerName(account.Provider)

	// Get or create sync cursor.
	cursor, err := e.progress.GetOrCreateCursor(ctx, account.ID, provider)
	if err != nil {
		return fmt.Errorf("getting sync cursor: %w", err)
	}

	// Route to the appropriate syncer.
	switch provider {
	case "imap":
		if imapClient == nil {
			return fmt.Errorf("IMAP client required for provider %q", account.Provider)
		}
		err = e.imapSync.Sync(ctx, account, imapClient, cursor.ID)

	case "gmail":
		if gmailSvc == nil {
			return fmt.Errorf("Gmail service required for provider %q", account.Provider)
		}
		err = e.gmailSync.Sync(ctx, account, gmailSvc, cursor.ID)

	default:
		return fmt.Errorf("unsupported provider: %q", account.Provider)
	}

	if err != nil {
		// Record the error in the cursor.
		if markErr := e.progress.MarkError(ctx, cursor.ID, err.Error()); markErr != nil {
			slog.Error("failed to mark sync error",
				"cursor_id", cursor.ID,
				"mark_error", markErr,
				"original_error", err,
			)
		}
		return fmt.Errorf("sync failed for account %s: %w", account.ID, err)
	}

	return nil
}

// Progress returns the ProgressTracker for external progress queries.
func (e *SyncEngine) Progress() *ProgressTracker {
	return e.progress
}

// providerName normalizes the provider string to the cursor provider value.
// 'outlook' uses IMAP under the hood.
func providerName(provider string) string {
	switch provider {
	case "gmail":
		return "gmail"
	case "imap", "outlook":
		return "imap"
	default:
		return provider
	}
}

// sinceDate returns the date 30 days ago (used for initial sync window).
func sinceDate() time.Time {
	return time.Now().AddDate(0, 0, -syncWindow)
}
