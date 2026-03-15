package gmail

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
)

// PushNotification represents a Gmail Pub/Sub push notification.
// CRITICAL: historyId is a JSON number, NOT a string.
type PushNotification struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    uint64 `json:"historyId"`
}

// SyncSignaler is called when a Gmail push notification is received.
type SyncSignaler interface {
	SignalGmailSync(emailAddress string, historyID uint64)
}

// PushHandler handles POST /api/v1/webhooks/gmail.
// Always returns 200 to acknowledge — Pub/Sub retries on non-2xx.
type PushHandler struct {
	Signaler SyncSignaler
}

// NewPushHandler creates a new PushHandler.
func NewPushHandler(signaler SyncSignaler) *PushHandler {
	return &PushHandler{Signaler: signaler}
}

func (h *PushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Always acknowledge — Pub/Sub retries on non-2xx
	defer func() {
		w.WriteHeader(http.StatusOK)
	}()

	var envelope struct {
		Message struct {
			Data      string `json:"data"`
			MessageID string `json:"messageId"`
		} `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		slog.Error("decoding pub/sub envelope", "error", err)
		return
	}

	rawData, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		slog.Error("decoding pub/sub data", "error", err)
		return
	}

	var notif PushNotification
	if err := json.Unmarshal(rawData, &notif); err != nil {
		slog.Error("parsing push notification", "error", err)
		return
	}

	slog.Debug("gmail push notification received",
		"email", notif.EmailAddress,
		"history_id", notif.HistoryID,
	)

	if h.Signaler != nil {
		h.Signaler.SignalGmailSync(notif.EmailAddress, notif.HistoryID)
	}
}
