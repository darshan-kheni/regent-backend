-- Phase 5: Digest delivery history
CREATE TABLE IF NOT EXISTS digest_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email_count INT NOT NULL DEFAULT 0,
    urgent_count INT NOT NULL DEFAULT 0,
    needs_reply_count INT NOT NULL DEFAULT 0,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    opened_at TIMESTAMPTZ,
    html_size_kb INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_digest_history_user_id ON digest_history(user_id);
CREATE INDEX idx_digest_history_tenant_created ON digest_history(tenant_id, created_at);

ALTER TABLE digest_history ENABLE ROW LEVEL SECURITY;
CREATE POLICY digest_history_tenant_isolation ON digest_history
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
