CREATE TABLE emails (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    account_id      UUID NOT NULL REFERENCES user_accounts(id),
    message_id      TEXT NOT NULL,
    thread_id       UUID,
    uid             BIGINT NOT NULL,
    folder          TEXT DEFAULT 'INBOX',
    direction       TEXT CHECK(direction IN ('inbound','outbound')),
    from_address    TEXT NOT NULL,
    from_name       TEXT,
    to_addresses    JSONB DEFAULT '[]',
    cc_addresses    JSONB DEFAULT '[]',
    subject         TEXT,
    body_text       TEXT,
    body_html       TEXT,
    has_attachments BOOLEAN DEFAULT false,
    attachments     JSONB DEFAULT '[]',
    headers         JSONB DEFAULT '{}',
    received_at     TIMESTAMPTZ NOT NULL,
    is_read         BOOLEAN DEFAULT false,
    is_starred      BOOLEAN DEFAULT false,
    raw_size        INT,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(account_id, uid)
);
CREATE INDEX idx_emails_tenant_date ON emails(tenant_id, received_at DESC);
CREATE INDEX idx_emails_account_uid ON emails(tenant_id, account_id, uid);
CREATE INDEX idx_emails_thread ON emails(thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX idx_emails_message_id ON emails(message_id);
CREATE INDEX idx_emails_unread ON emails(tenant_id, account_id) WHERE NOT is_read;
ALTER TABLE emails ENABLE ROW LEVEL SECURITY;
CREATE POLICY emails_tenant_isolation ON emails
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
