package send

import (
	"encoding/base64"
	"fmt"

	gmail "google.golang.org/api/gmail/v1"
)

// SendViaGmailAPI sends a raw MIME message using the Gmail API.
// It returns the server-assigned message ID.
func SendViaGmailAPI(svc *gmail.Service, userEmail string, rawMsg []byte) (string, error) {
	encoded := base64.URLEncoding.EncodeToString(rawMsg)

	msg := &gmail.Message{
		Raw: encoded,
	}

	sent, err := svc.Users.Messages.Send(userEmail, msg).Do()
	if err != nil {
		return "", fmt.Errorf("gmail send: %w", err)
	}

	return sent.ServerResponse.Header.Get("Message-Id"), nil
}
