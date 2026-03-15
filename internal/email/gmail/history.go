package gmail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// SyncIncremental fetches changes since lastHistoryID using history.list.
// Returns the new historyID to store for next sync.
// On 404 (historyId too old), returns 0 indicating a full sync is needed.
func SyncIncremental(ctx context.Context, svc *gmail.Service, email string, lastHistoryID uint64, handler HistoryHandler) (uint64, bool, error) {
	newHistoryID := lastHistoryID
	needsFullSync := false

	err := svc.Users.History.List(email).
		StartHistoryId(lastHistoryID).
		HistoryTypes("messageAdded", "messageDeleted", "labelAdded", "labelRemoved").
		MaxResults(500).
		Pages(ctx, func(page *gmail.ListHistoryResponse) error {
			if page.HistoryId > newHistoryID {
				newHistoryID = page.HistoryId
			}
			for _, h := range page.History {
				for _, added := range h.MessagesAdded {
					if err := handler.OnMessageAdded(ctx, added.Message.Id); err != nil {
						slog.Error("processing gmail message added", "id", added.Message.Id, "error", err)
					}
				}
				for _, deleted := range h.MessagesDeleted {
					handler.OnMessageDeleted(ctx, deleted.Message.Id)
				}
			}
			return nil
		})

	if err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 404 {
			slog.Warn("gmail history too old, full sync needed", "email", email)
			return 0, true, nil
		}
		return lastHistoryID, false, fmt.Errorf("history list: %w", err)
	}

	return newHistoryID, needsFullSync, nil
}

// HistoryHandler processes Gmail history events.
type HistoryHandler interface {
	OnMessageAdded(ctx context.Context, messageID string) error
	OnMessageDeleted(ctx context.Context, messageID string)
}
