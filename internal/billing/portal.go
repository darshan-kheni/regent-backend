package billing

import (
	"fmt"

	"github.com/stripe/stripe-go/v84"
	portalsession "github.com/stripe/stripe-go/v84/billingportal/session"
)

// CreatePortalSession creates a Stripe Billing Portal session for an existing
// customer. Returns the portal URL for the frontend to redirect to.
//
// The portal allows customers to manage payment methods, view invoices,
// and cancel/change subscriptions.
func CreatePortalSession(customerID string, cfg BillingConfig) (string, error) {
	if customerID == "" {
		return "", fmt.Errorf("customer ID is required for portal session")
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(cfg.FrontendURL + "/billing"),
	}

	session, err := portalsession.New(params)
	if err != nil {
		return "", fmt.Errorf("creating portal session: %w", err)
	}

	return session.URL, nil
}
