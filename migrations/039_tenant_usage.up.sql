-- Phase 7: Per-tenant usage metering
CREATE TABLE tenant_usage (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    period              TEXT NOT NULL CHECK (period IN ('daily','monthly')),
    period_date         DATE NOT NULL,
    emails_processed    INT DEFAULT 0,
    ai_calls            INT DEFAULT 0,
    tokens_consumed     BIGINT DEFAULT 0,
    storage_bytes       BIGINT DEFAULT 0,
    notifications_sent  INT DEFAULT 0,
    created_at          TIMESTAMPTZ DEFAULT now(),
    UNIQUE(tenant_id, period, period_date)
);

CREATE INDEX idx_usage_tenant_period ON tenant_usage (tenant_id, period, period_date DESC);
ALTER TABLE tenant_usage ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_usage_isolation ON tenant_usage
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
