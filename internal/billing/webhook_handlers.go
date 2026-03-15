package billing

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"

	"github.com/darshan-kheni/regent/internal/database"
)

// handleCheckoutCompleted processes a successful Stripe Checkout session.
// Extracts tenant_id and user_id from metadata, then provisions the plan.
func (h *WebhookHandler) handleCheckoutCompleted(ctx context.Context, event stripe.Event) error {
	var session stripe.CheckoutSession
	if err := unmarshalEventData(event, &session); err != nil {
		return fmt.Errorf("unmarshaling checkout session: %w", err)
	}

	tenantIDStr, ok := session.Metadata["tenant_id"]
	if !ok || tenantIDStr == "" {
		return fmt.Errorf("checkout session missing tenant_id metadata")
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return fmt.Errorf("parsing tenant_id: %w", err)
	}

	userIDStr := session.Metadata["user_id"]
	userID, _ := uuid.Parse(userIDStr) // May be empty for org-level checkout

	// Resolve price ID to plan name
	if session.Subscription == nil {
		return fmt.Errorf("checkout session has no subscription")
	}

	// Get the subscription to find the price ID
	subID := session.Subscription.ID
	customerID := ""
	if session.Customer != nil {
		customerID = session.Customer.ID
	}

	// Look up the plan from the price in the line items metadata
	planName := session.Metadata["plan"]
	if planName == "" {
		// Fallback: try to resolve from subscription metadata
		slog.Warn("billing: checkout session missing plan metadata, using free",
			"session_id", session.ID,
		)
		planName = PlanFree
	}

	tc := database.WithTenant(ctx, tenantID, userID)

	if err := ProvisionPlan(tc, h.pool, h.rdb, planName, customerID, subID); err != nil {
		return fmt.Errorf("provisioning plan: %w", err)
	}

	slog.Info("billing: checkout completed, plan provisioned",
		"tenant_id", tenantID,
		"plan", planName,
		"customer_id", customerID,
		"subscription_id", subID,
	)
	return nil
}

// handleInvoicePaid processes a successful invoice payment.
// Resets failure count and marks payment status as active.
func (h *WebhookHandler) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	var invoice stripe.Invoice
	if err := unmarshalEventData(event, &invoice); err != nil {
		return fmt.Errorf("unmarshaling invoice: %w", err)
	}

	if invoice.Customer == nil {
		return fmt.Errorf("invoice has no customer")
	}
	customerID := invoice.Customer.ID

	tag, err := h.pool.Exec(ctx, `
		UPDATE tenants
		SET payment_status = 'active',
		    failure_count = 0,
		    grace_period_ends = NULL,
		    updated_at = NOW()
		WHERE stripe_customer_id = $1
	`, customerID)
	if err != nil {
		return fmt.Errorf("updating tenant payment status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("billing: invoice.paid for unknown customer", "customer_id", customerID)
		return nil
	}

	slog.Info("billing: invoice paid, payment status reset",
		"customer_id", customerID,
	)
	return nil
}

// handleInvoiceFailed processes a failed invoice payment.
// Delegates to the dunning system for escalation.
func (h *WebhookHandler) handleInvoiceFailed(ctx context.Context, event stripe.Event) error {
	var invoice stripe.Invoice
	if err := unmarshalEventData(event, &invoice); err != nil {
		return fmt.Errorf("unmarshaling invoice: %w", err)
	}

	if invoice.Customer == nil {
		return fmt.Errorf("invoice has no customer")
	}
	customerID := invoice.Customer.ID

	if err := HandlePaymentFailure(ctx, h.pool, h.rdb, customerID); err != nil {
		return fmt.Errorf("handling payment failure: %w", err)
	}

	return nil
}

// handleSubscriptionUpdated processes subscription plan changes.
// Compares new price IDs to determine the new plan and adjusts limits.
func (h *WebhookHandler) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	var sub stripe.Subscription
	if err := unmarshalEventData(event, &sub); err != nil {
		return fmt.Errorf("unmarshaling subscription: %w", err)
	}

	if sub.Customer == nil {
		return fmt.Errorf("subscription has no customer")
	}
	customerID := sub.Customer.ID

	// Find the active price ID from subscription items
	var priceID string
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		priceID = sub.Items.Data[0].Price.ID
	}
	if priceID == "" {
		return fmt.Errorf("subscription has no price ID")
	}

	// Resolve price to plan
	newPlan, err := ResolvePriceIDToPlan(priceID)
	if err != nil {
		return fmt.Errorf("resolving price to plan: %w", err)
	}

	// Find tenant by customer ID
	tenantID, userID, err := h.findTenantByCustomer(ctx, customerID)
	if err != nil {
		return fmt.Errorf("finding tenant: %w", err)
	}

	tc := database.WithTenant(ctx, tenantID, userID)
	if err := AdjustLimits(tc, h.pool, h.rdb, newPlan); err != nil {
		return fmt.Errorf("adjusting limits: %w", err)
	}

	slog.Info("billing: subscription updated",
		"tenant_id", tenantID,
		"new_plan", newPlan,
		"customer_id", customerID,
	)
	return nil
}

// handleSubscriptionDeleted processes a cancelled subscription.
// Starts a 7-day grace period before downgrading to free.
func (h *WebhookHandler) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) error {
	var sub stripe.Subscription
	if err := unmarshalEventData(event, &sub); err != nil {
		return fmt.Errorf("unmarshaling subscription: %w", err)
	}

	if sub.Customer == nil {
		return fmt.Errorf("subscription has no customer")
	}
	customerID := sub.Customer.ID

	if err := StartGracePeriod(ctx, h.pool, customerID); err != nil {
		return fmt.Errorf("starting grace period: %w", err)
	}

	slog.Info("billing: subscription deleted, grace period started",
		"customer_id", customerID,
	)
	return nil
}

// handleInvoiceFinalized upserts invoice data into invoice_history for record keeping.
func (h *WebhookHandler) handleInvoiceFinalized(ctx context.Context, event stripe.Event) error {
	var invoice stripe.Invoice
	if err := unmarshalEventData(event, &invoice); err != nil {
		return fmt.Errorf("unmarshaling invoice: %w", err)
	}

	if invoice.Customer == nil {
		return fmt.Errorf("invoice has no customer")
	}
	customerID := invoice.Customer.ID

	amountDue := invoice.AmountDue
	currency := ""
	if invoice.Currency != "" {
		currency = string(invoice.Currency)
	}
	hostedURL := ""
	if invoice.HostedInvoiceURL != "" {
		hostedURL = invoice.HostedInvoiceURL
	}
	pdfURL := ""
	if invoice.InvoicePDF != "" {
		pdfURL = invoice.InvoicePDF
	}
	status := ""
	if invoice.Status != "" {
		status = string(invoice.Status)
	}

	_, err := h.pool.Exec(ctx, `
		INSERT INTO invoice_history (
			stripe_invoice_id, stripe_customer_id, amount_cents,
			currency, status, hosted_url, pdf_url, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (stripe_invoice_id) DO UPDATE SET
			amount_cents = EXCLUDED.amount_cents,
			currency = EXCLUDED.currency,
			status = EXCLUDED.status,
			hosted_url = EXCLUDED.hosted_url,
			pdf_url = EXCLUDED.pdf_url,
			updated_at = NOW()
	`, invoice.ID, customerID, amountDue, currency, status, hostedURL, pdfURL)
	if err != nil {
		return fmt.Errorf("upserting invoice history: %w", err)
	}

	slog.Info("billing: invoice finalized",
		"invoice_id", invoice.ID,
		"customer_id", customerID,
		"amount", amountDue,
	)
	return nil
}

// findTenantByCustomer looks up a tenant and user by Stripe customer ID.
func (h *WebhookHandler) findTenantByCustomer(ctx context.Context, customerID string) (uuid.UUID, uuid.UUID, error) {
	var tenantID, userID uuid.UUID
	err := h.pool.QueryRow(ctx, `
		SELECT t.id, COALESCE(
			(SELECT u.id FROM users u WHERE u.tenant_id = t.id LIMIT 1),
			'00000000-0000-0000-0000-000000000000'::uuid
		)
		FROM tenants t
		WHERE t.stripe_customer_id = $1
	`, customerID).Scan(&tenantID, &userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("finding tenant by customer %s: %w", customerID, err)
	}
	return tenantID, userID, nil
}
