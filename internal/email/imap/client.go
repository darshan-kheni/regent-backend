package imap

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// batchSize is the number of UIDs fetched in a single FETCH command.
const batchSize = 50

// FetchedMessage holds the raw data from an IMAP FETCH.
type FetchedMessage struct {
	UID      imap.UID
	Envelope *imap.Envelope
	Flags    []imap.Flag
	RawBody  []byte
}

// FetchMessages fetches messages by UID in batches of 50.
// Uses BODY.PEEK[] to avoid marking messages as \Seen.
// The client is NOT goroutine-safe — callers must ensure single-goroutine access.
func FetchMessages(client *imapclient.Client, uids []imap.UID) ([]*FetchedMessage, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	bodySection := &imap.FetchItemBodySection{
		Peek: true,
	}

	fetchOpts := &imap.FetchOptions{
		UID:         true,
		Envelope:    true,
		Flags:       true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	var results []*FetchedMessage

	for i := 0; i < len(uids); i += batchSize {
		end := min(i+batchSize, len(uids))
		batch := uids[i:end]

		uidSet := imap.UIDSetNum(batch...)

		cmd := client.Fetch(uidSet, fetchOpts)
		for {
			msg := cmd.Next()
			if msg == nil {
				break
			}

			buf, err := msg.Collect()
			if err != nil {
				slog.Warn("skipping unparseable message",
					"seq_num", msg.SeqNum,
					"error", err,
				)
				continue
			}

			fetched := &FetchedMessage{
				UID:      buf.UID,
				Envelope: buf.Envelope,
				Flags:    buf.Flags,
			}

			// Extract the raw body from the body section buffer.
			body := buf.FindBodySection(bodySection)
			if body != nil {
				fetched.RawBody = body
			}

			results = append(results, fetched)
		}

		if err := cmd.Close(); err != nil {
			return results, fmt.Errorf("FETCH batch [%d:%d]: %w", i, end, err)
		}
	}

	return results, nil
}

// SearchSince searches for UIDs of messages received since the given date.
// The IMAP SINCE criterion uses dates only (time component is ignored).
func SearchSince(client *imapclient.Client, since time.Time) ([]imap.UID, error) {
	criteria := &imap.SearchCriteria{
		Since: since,
	}

	cmd := client.UIDSearch(criteria, nil)
	data, err := cmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("UID SEARCH SINCE: %w", err)
	}

	return data.AllUIDs(), nil
}

// SelectMailbox selects a mailbox (e.g., "INBOX") for subsequent commands.
// Returns the mailbox status data including message counts.
func SelectMailbox(client *imapclient.Client, mailbox string) (*imap.SelectData, error) {
	data, err := client.Select(mailbox, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("SELECT %s: %w", mailbox, err)
	}
	return data, nil
}

// BatchCount returns the number of FETCH batches needed for the given UID count.
func BatchCount(totalUIDs int) int {
	if totalUIDs <= 0 {
		return 0
	}
	return (totalUIDs + batchSize - 1) / batchSize
}
