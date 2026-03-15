package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/darshan-kheni/regent/internal/billing"
)

// RegisterWebhookRoutes adds public webhook routes that do not require
// authentication. These are called by external services (Stripe, etc.).
func RegisterWebhookRoutes(r chi.Router, wh *billing.WebhookHandler) {
	r.Post("/api/v1/webhooks/stripe", wh.HandleStripeWebhook)
}
