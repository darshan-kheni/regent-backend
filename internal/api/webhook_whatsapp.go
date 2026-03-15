package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/darshan-kheni/regent/internal/briefings"
)

// whatsAppWebhookPayload represents the webhook payload from Meta.
type whatsAppWebhookPayload struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Statuses []struct {
					ID        string `json:"id"`
					Status    string `json:"status"` // sent, delivered, read, failed
					Timestamp string `json:"timestamp"`
					Errors    []struct {
						Code    int    `json:"code"`
						Title   string `json:"title"`
						Message string `json:"message"`
					} `json:"errors"`
				} `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// NewWhatsAppWebhookHandler creates a handler for WhatsApp delivery status callbacks.
// POST /webhooks/whatsapp/status — public route, no auth middleware.
func NewWhatsAppWebhookHandler(tracker *briefings.DeliveryTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload whatsAppWebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			slog.Error("whatsapp webhook: decode error", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		for _, entry := range payload.Entry {
			for _, change := range entry.Changes {
				for _, status := range change.Value.Statuses {
					dbStatus := mapWhatsAppStatus(status.Status)

					var deliveredAt, readAt *time.Time
					now := time.Now()

					switch dbStatus {
					case "delivered":
						deliveredAt = &now
					case "read":
						deliveredAt = &now
						readAt = &now
					}

					if tracker != nil {
						if err := tracker.UpdateStatus(r.Context(), status.ID, dbStatus, deliveredAt, readAt); err != nil {
							slog.Error("whatsapp webhook: update status",
								"external_id", status.ID, "error", err)
						}
					}

					if len(status.Errors) > 0 {
						slog.Warn("whatsapp webhook: delivery error",
							"external_id", status.ID,
							"error_code", status.Errors[0].Code,
							"error_title", status.Errors[0].Title,
						)
					}

					slog.Info("whatsapp webhook: status update",
						"external_id", status.ID,
						"status", status.Status,
					)
				}
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func mapWhatsAppStatus(waStatus string) string {
	switch waStatus {
	case "sent":
		return "sent"
	case "delivered":
		return "delivered"
	case "read":
		return "read"
	case "failed":
		return "failed"
	default:
		return "sent"
	}
}
