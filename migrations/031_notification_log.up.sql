CREATE TABLE notification_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    briefing_id UUID,
    email_id UUID REFERENCES emails(id) ON DELETE SET NULL,
    channel TEXT NOT NULL CHECK (channel IN ('sms','whatsapp','signal','push','email_digest')),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued','sent','delivered','failed','read')),
    priority INT NOT NULL DEFAULT 50,
    message_body TEXT,
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    error_message TEXT,
    cost_cents INT NOT NULL DEFAULT 0,
    external_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_log_user_created
    ON notification_log(tenant_id, user_id, created_at DESC);
CREATE INDEX idx_notification_log_external_id
    ON notification_log(external_id) WHERE external_id IS NOT NULL;
CREATE INDEX idx_notification_log_status
    ON notification_log(status) WHERE status IN ('queued','sent');

ALTER TABLE notification_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY notification_log_tenant_isolation ON notification_log
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
