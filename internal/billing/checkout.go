package billing

import (
	"fmt"

	"github.com/stripe/stripe-go/v84"
	checkoutsession "github.com/stripe/stripe-go/v84/checkout/session"

	"github.com/darshan-kheni/regent/internal/database"
)

// CreateCheckoutSession creates a Stripe Checkout session for a subscription.
// Returns the checkout URL that the frontend should redirect to.
//
// priceID must correspond to a non-free plan. The tenant_id and user_id are
// stored in session metadata for the webhook to provision the plan.
func CreateCheckoutSession(tc database.TenantContext, cfg BillingConfig, priceID string) (string, error) {
	// Reject free tier — no checkout needed
	if priceID == cfg.StripePriceFree {
		return "", fmt.Errorf("cannot create checkout session for free plan")
	}

	// Resolve plan name for metadata
	planName, err := ResolvePriceIDToPlan(priceID)
	if err != nil {
		return "", fmt.Errorf("resolving plan: %w", err)
	}

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL:          stripe.String(cfg.FrontendURL + "/billing?status=success&session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:           stripe.String(cfg.FrontendURL + "/billing?status=cancelled"),
		AllowPromotionCodes: stripe.Bool(true),
		Metadata: map[string]string{
			"tenant_id": tc.TenantID.String(),
			"user_id":   tc.UserID.String(),
			"plan":      planName,
		},
	}

	session, err := checkoutsession.New(params)
	if err != nil {
		return "", fmt.Errorf("creating checkout session: %w", err)
	}

	return session.URL, nil
}
