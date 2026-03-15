package billing

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/customer"
	"github.com/stripe/stripe-go/v84/paymentmethod"
	"github.com/stripe/stripe-go/v84/setupintent"

	"github.com/darshan-kheni/regent/internal/database"
)

// PaymentMethod represents a stored payment method (card).
type PaymentMethod struct {
	ID        string `json:"id"`
	Last4     string `json:"last4"`
	Brand     string `json:"brand"`
	ExpMonth  int    `json:"exp_month"`
	ExpYear   int    `json:"exp_year"`
	IsDefault bool   `json:"is_default"`
}

// ListPaymentMethods returns the payment methods on file for the tenant's Stripe customer.
// Returns an empty slice for tenants without a Stripe customer (free tier).
func ListPaymentMethods(ctx database.TenantContext, pool *pgxpool.Pool) ([]PaymentMethod, error) {
	customerID, err := getCustomerID(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("getting Stripe customer: %w", err)
	}
	if customerID == "" {
		return []PaymentMethod{}, nil
	}

	// Get the default payment method for comparison
	defaultPMID, err := getDefaultPaymentMethodID(customerID)
	if err != nil {
		slog.Warn("billing: failed to get default payment method, continuing",
			"customer_id", customerID,
			"error", err,
		)
	}

	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(customerID),
		Type:     stripe.String(string(stripe.PaymentMethodTypeCard)),
	}

	var methods []PaymentMethod
	iter := paymentmethod.List(params)
	for iter.Next() {
		pm := iter.PaymentMethod()
		if pm.Card == nil {
			continue
		}
		methods = append(methods, PaymentMethod{
			ID:        pm.ID,
			Last4:     pm.Card.Last4,
			Brand:     string(pm.Card.Brand),
			ExpMonth:  int(pm.Card.ExpMonth),
			ExpYear:   int(pm.Card.ExpYear),
			IsDefault: pm.ID == defaultPMID,
		})
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("listing payment methods: %w", err)
	}

	return methods, nil
}

// CreateSetupIntent creates a Stripe SetupIntent for the tenant so they can
// add a new payment method. Returns the client_secret for the frontend.
func CreateSetupIntent(ctx database.TenantContext, pool *pgxpool.Pool) (string, error) {
	customerID, err := getCustomerID(ctx, pool)
	if err != nil {
		return "", fmt.Errorf("getting Stripe customer: %w", err)
	}
	if customerID == "" {
		return "", fmt.Errorf("no billing account found; upgrade from free tier first")
	}

	si, err := setupintent.New(&stripe.SetupIntentParams{
		Customer: stripe.String(customerID),
		PaymentMethodTypes: stripe.StringSlice([]string{
			string(stripe.PaymentMethodTypeCard),
		}),
	})
	if err != nil {
		return "", fmt.Errorf("creating SetupIntent: %w", err)
	}

	slog.Info("billing: created SetupIntent",
		"setup_intent_id", si.ID,
		"tenant_id", ctx.TenantID,
	)

	return si.ClientSecret, nil
}

// getCustomerID fetches the Stripe customer ID for a tenant.
func getCustomerID(ctx database.TenantContext, pool *pgxpool.Pool) (string, error) {
	var customerID *string
	err := pool.QueryRow(ctx,
		"SELECT stripe_customer_id FROM tenants WHERE id = $1",
		ctx.TenantID,
	).Scan(&customerID)
	if err != nil {
		return "", fmt.Errorf("querying tenant: %w", err)
	}
	if customerID == nil {
		return "", nil
	}
	return *customerID, nil
}

// getDefaultPaymentMethodID returns the ID of the customer's default payment method
// by reading InvoiceSettings.DefaultPaymentMethod from the Stripe customer object.
func getDefaultPaymentMethodID(customerID string) (string, error) {
	cust, err := customer.Get(customerID, nil)
	if err != nil {
		return "", fmt.Errorf("fetching Stripe customer: %w", err)
	}
	if cust.InvoiceSettings != nil && cust.InvoiceSettings.DefaultPaymentMethod != nil {
		return cust.InvoiceSettings.DefaultPaymentMethod.ID, nil
	}
	return "", nil
}
