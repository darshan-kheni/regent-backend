package gmail

import (
	"context"
	"encoding/base64"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

// ListMessages lists message IDs matching a query, with pagination.
func ListMessages(ctx context.Context, svc *gmail.Service, email, query string) ([]string, error) {
	var messageIDs []string
	err := svc.Users.Messages.List(email).
		Q(query).
		MaxResults(500).
		Pages(ctx, func(page *gmail.ListMessagesResponse) error {
			for _, msg := range page.Messages {
				messageIDs = append(messageIDs, msg.Id)
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	return messageIDs, nil
}

// GetRawMessage fetches a message in raw format for unified parsing via go-message.
func GetRawMessage(ctx context.Context, svc *gmail.Service, email, messageID string) ([]byte, error) {
	msg, err := svc.Users.Messages.Get(email, messageID).Format("raw").Do()
	if err != nil {
		return nil, fmt.Errorf("get message %s: %w", messageID, err)
	}
	raw, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		return nil, fmt.Errorf("decode raw message: %w", err)
	}
	return raw, nil
}
