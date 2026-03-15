package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/invoice"

	"github.com/darshan-kheni/regent/internal/database"
)

// Invoice represents a billing invoice for display.
type Invoice struct {
	ID          string `json:"id"`
	AmountCents int    `json:"amount_cents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	PdfURL      string `json:"pdf_url"`
	CreatedAt   string `json:"created_at"`
}

// InvoiceDetail includes line items for a single invoice.
type InvoiceDetail struct {
	Invoice
	LineItems []LineItem `json:"line_items"`
}

// LineItem represents a single charge within an invoice.
type LineItem struct {
	Description string `json:"description"`
	Amount      int    `json:"amount"`
	Quantity    int    `json:"quantity"`
}

// InvoiceService handles invoice retrieval and synchronization.
type InvoiceService struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// NewInvoiceService creates a new InvoiceService.
func NewInvoiceService(pool *pgxpool.Pool, rdb *redis.Client) *InvoiceService {
	return &InvoiceService{pool: pool, rdb: rdb}
}

// ListInvoices returns invoices for the authenticated tenant.
// First tries the local cache (invoice_history), then falls back to Stripe API.
func (s *InvoiceService) ListInvoices(ctx database.TenantContext) ([]Invoice, error) {
	// Try local cache first
	invoices, err := s.listFromDB(ctx)
	if err != nil {
		slog.Warn("billing: failed to query local invoices, falling back to Stripe",
			"tenant_id", ctx.TenantID,
			"error", err,
		)
	}
	if len(invoices) > 0 {
		return invoices, nil
	}

	// Fallback: fetch from Stripe
	customerID, err := s.getStripeCustomerID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Stripe customer ID: %w", err)
	}
	if customerID == "" {
		// Free tier with no Stripe customer — return empty array
		return []Invoice{}, nil
	}

	return s.listFromStripe(customerID)
}

// GetInvoice retrieves a single invoice with line items from Stripe.
func (s *InvoiceService) GetInvoice(ctx database.TenantContext, invoiceID string) (*InvoiceDetail, error) {
	// Verify this invoice belongs to the tenant
	customerID, err := s.getStripeCustomerID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Stripe customer ID: %w", err)
	}
	if customerID == "" {
		return nil, fmt.Errorf("no billing account found")
	}

	params := &stripe.InvoiceParams{}
	params.AddExpand("lines")
	inv, err := invoice.Get(invoiceID, params)
	if err != nil {
		return nil, fmt.Errorf("fetching invoice from Stripe: %w", err)
	}

	// Verify the invoice belongs to this customer
	if inv.Customer.ID != customerID {
		return nil, fmt.Errorf("invoice not found")
	}

	detail := &InvoiceDetail{
		Invoice: invoiceFromStripe(inv),
	}

	if inv.Lines != nil {
		for _, li := range inv.Lines.Data {
			desc := ""
			if li.Description != "" {
				desc = li.Description
			}
			qty := int(li.Quantity)
			if qty == 0 {
				qty = 1
			}
			detail.LineItems = append(detail.LineItems, LineItem{
				Description: desc,
				Amount:      int(li.Amount),
				Quantity:    qty,
			})
		}
	}

	return detail, nil
}

// SyncInvoice upserts a Stripe invoice event into the local invoice_history table.
// Called from webhook handlers when invoice events are received.
func SyncInvoice(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID, inv *stripe.Invoice) error {
	if inv == nil {
		return fmt.Errorf("nil invoice")
	}

	var periodStart, periodEnd *time.Time
	if inv.PeriodStart > 0 {
		t := time.Unix(inv.PeriodStart, 0).UTC()
		periodStart = &t
	}
	if inv.PeriodEnd > 0 {
		t := time.Unix(inv.PeriodEnd, 0).UTC()
		periodEnd = &t
	}

	pdfURL := ""
	if inv.InvoicePDF != "" {
		pdfURL = inv.InvoicePDF
	}
	hostedURL := ""
	if inv.HostedInvoiceURL != "" {
		hostedURL = inv.HostedInvoiceURL
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO invoice_history (tenant_id, stripe_invoice_id, amount_due, amount_paid, currency, status, period_start, period_end, invoice_pdf, hosted_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (stripe_invoice_id) DO UPDATE SET
		     amount_due = EXCLUDED.amount_due,
		     amount_paid = EXCLUDED.amount_paid,
		     status = EXCLUDED.status,
		     invoice_pdf = EXCLUDED.invoice_pdf,
		     hosted_url = EXCLUDED.hosted_url`,
		tenantID, inv.ID, inv.AmountDue, inv.AmountPaid,
		string(inv.Currency), string(inv.Status),
		periodStart, periodEnd, pdfURL, hostedURL,
	)
	if err != nil {
		return fmt.Errorf("upserting invoice: %w", err)
	}

	slog.Info("billing: synced invoice",
		"stripe_invoice_id", inv.ID,
		"tenant_id", tenantID,
		"status", inv.Status,
	)

	return nil
}

// listFromDB queries the local invoice_history cache.
func (s *InvoiceService) listFromDB(ctx database.TenantContext) ([]Invoice, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT stripe_invoice_id, amount_due, currency, status,
		        period_start, period_end, invoice_pdf, created_at
		 FROM invoice_history
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC
		 LIMIT 50`,
		ctx.TenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying invoices: %w", err)
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		var periodStart, periodEnd *time.Time
		var createdAt time.Time
		if err := rows.Scan(
			&inv.ID, &inv.AmountCents, &inv.Currency, &inv.Status,
			&periodStart, &periodEnd, &inv.PdfURL, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning invoice: %w", err)
		}
		if periodStart != nil {
			inv.PeriodStart = periodStart.Format(time.RFC3339)
		}
		if periodEnd != nil {
			inv.PeriodEnd = periodEnd.Format(time.RFC3339)
		}
		inv.CreatedAt = createdAt.Format(time.RFC3339)
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating invoices: %w", err)
	}

	return invoices, nil
}

// listFromStripe fetches invoices directly from the Stripe API.
func (s *InvoiceService) listFromStripe(customerID string) ([]Invoice, error) {
	params := &stripe.InvoiceListParams{
		Customer: stripe.String(customerID),
	}
	params.Filters.AddFilter("limit", "", "50")

	var invoices []Invoice
	iter := invoice.List(params)
	for iter.Next() {
		inv := iter.Invoice()
		invoices = append(invoices, invoiceFromStripe(inv))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("listing Stripe invoices: %w", err)
	}

	return invoices, nil
}

// getStripeCustomerID fetches the Stripe customer ID for the tenant.
func (s *InvoiceService) getStripeCustomerID(ctx database.TenantContext) (string, error) {
	var customerID *string
	err := s.pool.QueryRow(ctx,
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

// invoiceFromStripe converts a Stripe invoice to our Invoice type.
func invoiceFromStripe(inv *stripe.Invoice) Invoice {
	result := Invoice{
		ID:          inv.ID,
		AmountCents: int(inv.AmountDue),
		Currency:    string(inv.Currency),
		Status:      string(inv.Status),
		PdfURL:      inv.InvoicePDF,
	}
	if inv.PeriodStart > 0 {
		result.PeriodStart = time.Unix(inv.PeriodStart, 0).UTC().Format(time.RFC3339)
	}
	if inv.PeriodEnd > 0 {
		result.PeriodEnd = time.Unix(inv.PeriodEnd, 0).UTC().Format(time.RFC3339)
	}
	if inv.Created > 0 {
		result.CreatedAt = time.Unix(inv.Created, 0).UTC().Format(time.RFC3339)
	}
	return result
}
