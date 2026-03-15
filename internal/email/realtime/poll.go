package realtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"

	imappkg "github.com/darshan-kheni/regent/internal/email/imap"
)

// pollInterval is the time between NOOP polls.
const pollInterval = 2 * time.Minute

// Poller implements fallback polling for providers that do not support IMAP IDLE.
// It sends NOOP every 2 minutes, which prompts the server to report mailbox
// changes (EXISTS), then fetches any new messages using the same pipeline
// as IDLEWatcher.
type Poller struct {
	watcher *IDLEWatcher
}

// NewPoller creates a Poller that reuses the IDLEWatcher processing pipeline.
func NewPoller(watcher *IDLEWatcher) *Poller {
	return &Poller{watcher: watcher}
}

// RunPoll enters the poll loop: NOOP → check for new mail → sleep 2 minutes → repeat.
// The loop runs until the context is cancelled.
func (p *Poller) RunPoll(ctx context.Context, client *imapclient.Client) error {
	if _, err := imappkg.SelectMailbox(client, "INBOX"); err != nil {
		return fmt.Errorf("SELECT INBOX: %w", err)
	}

	slog.Info("poll watcher started",
		"account_id", p.watcher.account.ID,
		"email", p.watcher.account.EmailAddress,
		"interval", pollInterval,
	)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := client.Noop().Wait(); err != nil {
				slog.Error("NOOP poll failed",
					"account_id", p.watcher.account.ID,
					"error", err,
				)
				return fmt.Errorf("NOOP: %w", err)
			}
			p.watcher.fetchNewEmails(ctx, client)
		}
	}
}
