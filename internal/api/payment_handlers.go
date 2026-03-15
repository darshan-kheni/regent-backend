package api

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/billing"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// PaymentHandlers contains HTTP handlers for payment method operations.
type PaymentHandlers struct {
	pool *pgxpool.Pool
}

// NewPaymentHandlers creates a new PaymentHandlers.
func NewPaymentHandlers(pool *pgxpool.Pool) *PaymentHandlers {
	return &PaymentHandlers{pool: pool}
}

// HandleListPaymentMethods returns the tenant's payment methods on file.
// GET /api/v1/billing/payment-methods
func (h *PaymentHandlers) HandleListPaymentMethods(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	methods, err := billing.ListPaymentMethods(tc, h.pool)
	if err != nil {
		slog.Error("payment-methods: list error",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "QUERY_FAILED", "failed to list payment methods")
		return
	}

	WriteJSON(w, r, http.StatusOK, methods)
}

// HandleSetupPaymentMethod creates a Stripe SetupIntent so the frontend can
// collect new payment method details.
// POST /api/v1/billing/payment-methods/setup
func (h *PaymentHandlers) HandleSetupPaymentMethod(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	clientSecret, err := billing.CreateSetupIntent(tc, h.pool)
	if err != nil {
		slog.Error("payment-methods: setup intent error",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusBadRequest, "SETUP_FAILED", err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{
		"client_secret": clientSecret,
	})
}
