package gmail

import (
	"context"
	"fmt"

	gmailapi "google.golang.org/api/gmail/v1"
)

// SetupWatch calls users.watch() to register Pub/Sub push notifications.
// Must be called on account connection and renewed daily (expires in 7 days).
func SetupWatch(ctx context.Context, svc *gmailapi.Service, email, topic string) (uint64, error) {
	resp, err := svc.Users.Watch(email, &gmailapi.WatchRequest{
		TopicName: topic,
		LabelIds:  []string{"INBOX"},
	}).Do()
	if err != nil {
		return 0, fmt.Errorf("watch setup for %s: %w", email, err)
	}
	return uint64(resp.HistoryId), nil
}

// StopWatch stops push notifications for an account.
func StopWatch(ctx context.Context, svc *gmailapi.Service, email string) error {
	return svc.Users.Stop(email).Do()
}
