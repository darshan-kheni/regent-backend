CREATE TABLE email_credentials (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id              UUID NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    credential_type         TEXT NOT NULL CHECK(credential_type IN ('imap_password','smtp_password','app_password')),
    encrypted_value         BYTEA NOT NULL,
    encryption_nonce        BYTEA NOT NULL,
    encryption_key_version  INT NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ DEFAULT now(),
    updated_at              TIMESTAMPTZ DEFAULT now(),
    UNIQUE(account_id, credential_type)
);
CREATE INDEX idx_email_creds_account ON email_credentials(tenant_id, account_id);
CREATE INDEX idx_email_creds_version ON email_credentials(encryption_key_version);
ALTER TABLE email_credentials ENABLE ROW LEVEL SECURITY;
CREATE POLICY email_creds_tenant_isolation ON email_credentials
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TRIGGER set_email_creds_updated_at BEFORE UPDATE ON email_credentials
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
