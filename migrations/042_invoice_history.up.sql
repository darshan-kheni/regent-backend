-- Phase 7: Local invoice cache
CREATE TABLE invoice_history (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    stripe_invoice_id   TEXT UNIQUE NOT NULL,
    amount_due          INT NOT NULL,
    amount_paid         INT NOT NULL DEFAULT 0,
    currency            TEXT DEFAULT 'usd',
    status              TEXT CHECK (status IN ('draft','open','paid','void','uncollectible')),
    period_start        TIMESTAMPTZ,
    period_end          TIMESTAMPTZ,
    invoice_pdf         TEXT,
    hosted_url          TEXT,
    created_at          TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_invoices_tenant ON invoice_history (tenant_id, created_at DESC);
ALTER TABLE invoice_history ENABLE ROW LEVEL SECURITY;
CREATE POLICY invoice_history_isolation ON invoice_history
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
