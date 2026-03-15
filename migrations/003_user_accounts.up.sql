CREATE TABLE user_accounts (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id               UUID NOT NULL REFERENCES tenants(id),
    provider                TEXT NOT NULL CHECK(provider IN ('gmail','outlook','imap')),
    email_address           TEXT NOT NULL,
    display_name            TEXT,
    credentials_encrypted   BYTEA,
    credentials_nonce       BYTEA,
    oauth_token_encrypted   BYTEA,
    oauth_refresh_encrypted BYTEA,
    imap_host               TEXT,
    imap_port               INT DEFAULT 993,
    smtp_host               TEXT,
    smtp_port               INT DEFAULT 587,
    color                   TEXT DEFAULT '#C9A96E',
    sync_status             TEXT DEFAULT 'pending'
                            CHECK(sync_status IN ('pending','syncing','active','error','paused')),
    last_sync_at            TIMESTAMPTZ,
    error_message           TEXT,
    created_at              TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_accounts_user ON user_accounts(user_id, tenant_id);
CREATE INDEX idx_accounts_sync ON user_accounts(sync_status)
    WHERE sync_status IN ('pending','syncing','error');
ALTER TABLE user_accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY accounts_tenant_isolation ON user_accounts
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
