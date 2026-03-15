package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/billing"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// BillingHandlers provides HTTP handlers for billing operations.
type BillingHandlers struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
	cfg  billing.BillingConfig
}

// NewBillingHandlers creates a new BillingHandlers instance.
func NewBillingHandlers(pool *pgxpool.Pool, rdb *redis.Client, cfg billing.BillingConfig) *BillingHandlers {
	return &BillingHandlers{
		pool: pool,
		rdb:  rdb,
		cfg:  cfg,
	}
}

// checkoutRequest is the JSON body for POST /billing/checkout.
type checkoutRequest struct {
	PriceID string `json:"price_id"`
}

// HandleCheckout creates a Stripe Checkout session and returns the URL.
// POST /api/v1/billing/checkout
func (h *BillingHandlers) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "tenant context required")
		return
	}

	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if req.PriceID == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_FIELD", "price_id is required")
		return
	}

	url, err := billing.CreateCheckoutSession(tc, h.cfg, req.PriceID)
	if err != nil {
		slog.Error("billing: failed to create checkout session",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "CHECKOUT_FAILED", "failed to create checkout session")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"url": url})
}

// HandlePortal creates a Stripe Billing Portal session and returns the URL.
// POST /api/v1/billing/portal
func (h *BillingHandlers) HandlePortal(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "tenant context required")
		return
	}

	// Get the tenant's Stripe customer ID
	var customerID string
	err := h.pool.QueryRow(r.Context(), `
		SELECT stripe_customer_id FROM tenants WHERE id = $1
	`, tc.TenantID).Scan(&customerID)
	if err != nil {
		slog.Error("billing: failed to get customer ID",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to look up billing info")
		return
	}
	if customerID == "" {
		WriteError(w, r, http.StatusBadRequest, "NO_SUBSCRIPTION", "no active subscription found")
		return
	}

	url, err := billing.CreatePortalSession(customerID, h.cfg)
	if err != nil {
		slog.Error("billing: failed to create portal session",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "PORTAL_FAILED", "failed to create portal session")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"url": url})
}

// subscriptionResponse matches the frontend Subscription interface.
type subscriptionResponse struct {
	PlanName           string   `json:"plan_name"`
	PlanID             string   `json:"plan_id"`
	Status             string   `json:"status"`
	CurrentPeriodEnd   string   `json:"current_period_end,omitempty"`
	Features           []string `json:"features"`
	PriceCents         int      `json:"price_cents"`
	StripeCustomerID   string   `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string `json:"stripe_subscription_id,omitempty"`
}

// planLabels maps internal plan names to display labels.
var planLabels = map[string]string{
	billing.PlanFree:         "Free",
	billing.PlanAttache:      "Attaché",
	billing.PlanPrivyCouncil: "Privy Council",
	billing.PlanEstate:       "Estate",
}

// HandleGetSubscription returns the current subscription info for the tenant.
// GET /api/v1/billing/subscription
func (h *BillingHandlers) HandleGetSubscription(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "tenant context required")
		return
	}

	var (
		plan, paymentStatus                       string
		stripeCustomerID, stripeSubscriptionID     *string
		planStartedAt, gracePeriodEnds             *string
	)

	err := h.pool.QueryRow(r.Context(), `
		SELECT plan,
		       COALESCE(payment_status, 'none'),
		       stripe_customer_id,
		       stripe_subscription_id,
		       TO_CHAR(plan_started_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       TO_CHAR(grace_period_ends, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM tenants WHERE id = $1
	`, tc.TenantID).Scan(&plan, &paymentStatus, &stripeCustomerID, &stripeSubscriptionID, &planStartedAt, &gracePeriodEnds)
	if err != nil {
		slog.Error("billing: failed to get subscription",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to get subscription info")
		return
	}

	// Map payment_status to frontend status values
	status := "active"
	switch paymentStatus {
	case "trialing":
		status = "trialing"
	case "past_due":
		status = "past_due"
	case "canceled", "cancelled":
		status = "canceled"
	default:
		status = "active"
	}

	// Build human-readable feature list
	featureKeys := billing.PlanFeatures[plan]
	features := make([]string, 0, len(featureKeys))
	for _, f := range featureKeys {
		features = append(features, billing.FeatureLabel(f))
	}

	resp := subscriptionResponse{
		PlanName: planLabels[plan],
		PlanID:   plan,
		Status:   status,
		Features: features,
	}

	// Look up price info
	if pt, found := billing.GetPlanByName(plan); found {
		resp.PriceCents = pt.PriceCents
	}

	if stripeCustomerID != nil {
		resp.StripeCustomerID = *stripeCustomerID
	}
	if stripeSubscriptionID != nil {
		resp.StripeSubscriptionID = *stripeSubscriptionID
	}
	if gracePeriodEnds != nil {
		resp.CurrentPeriodEnd = *gracePeriodEnds
	} else if planStartedAt != nil {
		// Fallback: use plan_started_at if no grace period
		resp.CurrentPeriodEnd = *planStartedAt
	}

	WriteJSON(w, r, http.StatusOK, resp)
}
