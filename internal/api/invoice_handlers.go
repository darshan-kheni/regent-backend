package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/darshan-kheni/regent/internal/billing"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// InvoiceHandlers contains HTTP handlers for invoice operations.
type InvoiceHandlers struct {
	invoices *billing.InvoiceService
}

// NewInvoiceHandlers creates a new InvoiceHandlers.
func NewInvoiceHandlers(invoices *billing.InvoiceService) *InvoiceHandlers {
	return &InvoiceHandlers{invoices: invoices}
}

// HandleListInvoices returns the tenant's invoice history.
// GET /api/v1/billing/invoices
func (h *InvoiceHandlers) HandleListInvoices(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	invoices, err := h.invoices.ListInvoices(tc)
	if err != nil {
		slog.Error("invoices: list error",
			"tenant_id", tc.TenantID,
			"error", err,
		)
		WriteError(w, r, http.StatusInternalServerError, "QUERY_FAILED", "failed to list invoices")
		return
	}

	WriteJSON(w, r, http.StatusOK, invoices)
}

// HandleGetInvoice returns a single invoice with line item details.
// GET /api/v1/billing/invoices/{id}
func (h *InvoiceHandlers) HandleGetInvoice(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	invoiceID := chi.URLParam(r, "id")
	if invoiceID == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_ID", "invoice ID is required")
		return
	}

	detail, err := h.invoices.GetInvoice(tc, invoiceID)
	if err != nil {
		slog.Error("invoices: get error",
			"tenant_id", tc.TenantID,
			"invoice_id", invoiceID,
			"error", err,
		)
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "invoice not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, detail)
}
