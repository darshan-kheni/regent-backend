package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/darshan-kheni/regent/internal/briefings"
)

// NewTwilioWebhookHandler creates a handler for Twilio SMS status callbacks.
// POST /webhooks/twilio/status — public route, no auth middleware.
func NewTwilioWebhookHandler(tracker *briefings.DeliveryTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		messageSid := r.FormValue("MessageSid")
		messageStatus := r.FormValue("MessageStatus")
		errorCode := r.FormValue("ErrorCode")

		if messageSid == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var status string
		var deliveredAt *time.Time
		now := time.Now()

		switch messageStatus {
		case "delivered":
			status = "delivered"
			deliveredAt = &now
		case "undelivered", "failed":
			status = "failed"
		case "sent":
			status = "sent"
		default:
			w.WriteHeader(http.StatusOK)
			return
		}

		if tracker != nil {
			if err := tracker.UpdateStatus(r.Context(), messageSid, status, deliveredAt, nil); err != nil {
				slog.Error("twilio webhook: update status", "sid", messageSid, "error", err)
			}
		}

		// Error codes 21211 (invalid number), 21614 (not mobile)
		if errorCode == "21211" || errorCode == "21614" {
			slog.Warn("twilio: invalid SMS number detected",
				"sid", messageSid, "error_code", errorCode)
		}

		w.WriteHeader(http.StatusOK)
	}
}
