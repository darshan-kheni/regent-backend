-- 015_email_send_log.up.sql
-- Tracks all outbound emails for auditing and rate limiting.

CREATE TABLE email_send_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id      UUID NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    draft_id        UUID REFERENCES draft_replies(id) ON DELETE SET NULL,
    email_id        UUID REFERENCES emails(id) ON DELETE SET NULL,
    to_addresses    JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(to_addresses) = 'array'),
    cc_addresses    JSONB DEFAULT '[]' CHECK(cc_addresses IS NULL OR jsonb_typeof(cc_addresses) = 'array'),
    subject         TEXT,
    method          TEXT NOT NULL CHECK(method IN ('gmail_api','smtp')),
    status          TEXT NOT NULL DEFAULT 'sent' CHECK(status IN ('sent','failed','bounced')),
    server_message_id TEXT,
    error_message   TEXT,
    sent_at         TIMESTAMPTZ DEFAULT now(),
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_send_log_tenant ON email_send_log(tenant_id, sent_at DESC);
CREATE INDEX idx_send_log_account ON email_send_log(account_id, sent_at DESC);
CREATE INDEX idx_send_log_rate ON email_send_log(user_id, sent_at DESC)
    WHERE status = 'sent';

ALTER TABLE email_send_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY send_log_tenant_isolation ON email_send_log
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
