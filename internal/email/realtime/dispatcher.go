package realtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jackc/pgx/v5/pgxpool"

	emailpkg "github.com/darshan-kheni/regent/internal/email"
	"github.com/darshan-kheni/regent/internal/email/mime"
	"github.com/darshan-kheni/regent/internal/models"
)

// Dispatcher selects and runs the appropriate realtime detection strategy
// for a given email account.
type Dispatcher struct {
	pool      *pgxpool.Pool
	threading *emailpkg.ThreadService
	dedup     *emailpkg.DedupService
	storage   mime.StorageUploader
}

// NewDispatcher creates a new Dispatcher.
func NewDispatcher(
	pool *pgxpool.Pool,
	threading *emailpkg.ThreadService,
	dedup *emailpkg.DedupService,
	storage mime.StorageUploader,
) *Dispatcher {
	return &Dispatcher{
		pool:      pool,
		threading: threading,
		dedup:     dedup,
		storage:   storage,
	}
}

// Run starts the appropriate realtime detection loop for the account.
// For IMAP accounts, it detects whether IDLE is supported and uses either
// IDLEWatcher or Poller. Gmail accounts should use the Gmail push pathway
// (not handled here — see gmail/push.go).
//
// The newMail channel is used for IDLE mode: the caller must wire
// imapclient.Options.UnilateralDataHandler.Mailbox to send on this channel
// when NumMessages increases.
//
// This method blocks until the context is cancelled or an unrecoverable
// error occurs.
func (d *Dispatcher) Run(
	ctx context.Context,
	account *models.UserAccount,
	client *imapclient.Client,
	capabilities []string,
	newMail <-chan struct{},
) error {
	strategy := DetectStrategy(account.Provider, capabilities)

	slog.Info("realtime dispatcher starting",
		"account_id", account.ID,
		"email", account.EmailAddress,
		"strategy", strategy,
	)

	watcher := NewIDLEWatcher(d.pool, d.threading, d.dedup, d.storage, account)

	switch strategy {
	case StrategyIDLE:
		return watcher.RunIDLE(ctx, client, newMail)

	case StrategyPoll:
		poller := NewPoller(watcher)
		return poller.RunPoll(ctx, client)

	case StrategyGmailPush:
		// Gmail push notifications are handled via Pub/Sub in gmail/push.go.
		// This dispatcher does not manage Gmail push — it is started separately
		// by the service orchestrator.
		return fmt.Errorf("gmail push strategy must be started via gmail/push.go, not dispatcher")

	default:
		return fmt.Errorf("unknown realtime strategy: %s", strategy)
	}
}
