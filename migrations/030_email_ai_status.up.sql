CREATE TABLE IF NOT EXISTS email_ai_status (
    email_id UUID PRIMARY KEY REFERENCES emails(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    plan TEXT NOT NULL DEFAULT 'free',
    stage TEXT NOT NULL DEFAULT 'queued'
        CHECK (stage IN ('queued','categorizing','summarizing','drafting','complete','error','skipped')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    retry_count INT NOT NULL DEFAULT 0,
    error_message TEXT,
    skipped_reason TEXT,
    CONSTRAINT fk_email_ai_status_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE INDEX idx_email_ai_status_pending ON email_ai_status (user_id) WHERE stage NOT IN ('complete', 'error', 'skipped');
CREATE INDEX idx_email_ai_status_skipped ON email_ai_status (user_id) WHERE stage = 'skipped';
CREATE INDEX idx_email_ai_status_tenant_created ON email_ai_status (tenant_id, started_at);

ALTER TABLE email_ai_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY email_ai_status_tenant_isolation ON email_ai_status
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
