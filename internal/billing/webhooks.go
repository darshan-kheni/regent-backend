package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

// maxWebhookBodySize is the maximum body size accepted from Stripe webhooks.
const maxWebhookBodySize = 65536

// WebhookHandler processes Stripe webhook events.
type WebhookHandler struct {
	pool          *pgxpool.Pool
	rdb           *redis.Client
	webhookSecret string
}

// NewWebhookHandler creates a new Stripe webhook handler.
func NewWebhookHandler(pool *pgxpool.Pool, rdb *redis.Client, webhookSecret string) *WebhookHandler {
	return &WebhookHandler{
		pool:          pool,
		rdb:           rdb,
		webhookSecret: webhookSecret,
	}
}

// HandleStripeWebhook is the HTTP handler for POST /api/v1/webhooks/stripe.
// It verifies the Stripe signature, deduplicates events, and routes to handlers.
func (h *WebhookHandler) HandleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("billing: failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify Stripe signature
	sig := r.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(body, sig, h.webhookSecret)
	if err != nil {
		slog.Warn("billing: invalid webhook signature", "error", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Deduplicate: INSERT INTO webhook_events ON CONFLICT DO NOTHING
	isDuplicate, err := h.deduplicateEvent(r.Context(), event.ID)
	if err != nil {
		slog.Error("billing: webhook dedup check failed", "event_id", event.ID, "error", err)
		// Continue processing — better to process twice than miss an event
	}
	if isDuplicate {
		slog.Info("billing: duplicate webhook event, skipping", "event_id", event.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Info("billing: processing webhook event",
		"event_id", event.ID,
		"type", event.Type,
	)

	// Route to handler by event type
	var handleErr error
	switch event.Type {
	case "checkout.session.completed":
		handleErr = h.handleCheckoutCompleted(r.Context(), event)
	case "invoice.paid":
		handleErr = h.handleInvoicePaid(r.Context(), event)
	case "invoice.payment_failed":
		handleErr = h.handleInvoiceFailed(r.Context(), event)
	case "customer.subscription.updated":
		handleErr = h.handleSubscriptionUpdated(r.Context(), event)
	case "customer.subscription.deleted":
		handleErr = h.handleSubscriptionDeleted(r.Context(), event)
	case "invoice.finalized":
		handleErr = h.handleInvoiceFinalized(r.Context(), event)
	default:
		slog.Debug("billing: unhandled webhook event type", "type", event.Type)
	}

	if handleErr != nil {
		slog.Error("billing: webhook handler failed",
			"event_id", event.ID,
			"type", event.Type,
			"error", handleErr,
		)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// deduplicateEvent inserts the event ID into webhook_events. Returns true if
// the event was already processed (duplicate).
func (h *WebhookHandler) deduplicateEvent(ctx context.Context, eventID string) (bool, error) {
	tag, err := h.pool.Exec(ctx, `
		INSERT INTO webhook_events (event_id, event_type, processed_at)
		VALUES ($1, '', NOW())
		ON CONFLICT (event_id) DO NOTHING
	`, eventID)
	if err != nil {
		return false, fmt.Errorf("dedup insert: %w", err)
	}
	// RowsAffected() == 0 means the row already existed (duplicate)
	return tag.RowsAffected() == 0, nil
}

// unmarshalEventData unmarshals the event's Data.Raw into the target.
func unmarshalEventData(event stripe.Event, target interface{}) error {
	if err := json.Unmarshal(event.Data.Raw, target); err != nil {
		return fmt.Errorf("unmarshaling event data: %w", err)
	}
	return nil
}
