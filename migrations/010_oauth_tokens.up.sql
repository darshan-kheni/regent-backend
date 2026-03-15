CREATE TABLE oauth_provider_tokens (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id               UUID NOT NULL REFERENCES tenants(id),
    provider                TEXT NOT NULL CHECK(provider IN ('google','microsoft')),
    access_token_encrypted  BYTEA NOT NULL,
    access_token_nonce      BYTEA NOT NULL,
    refresh_token_encrypted BYTEA NOT NULL,
    refresh_token_nonce     BYTEA NOT NULL,
    scopes                  TEXT[] NOT NULL,
    expires_at              TIMESTAMPTZ,
    provider_user_id        TEXT,
    provider_email          TEXT,
    last_refreshed_at       TIMESTAMPTZ DEFAULT now(),
    created_at              TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id, provider)
);

CREATE INDEX idx_oauth_tokens_user ON oauth_provider_tokens(user_id, provider);
CREATE INDEX idx_oauth_tokens_expiry ON oauth_provider_tokens(expires_at) WHERE expires_at IS NOT NULL;
ALTER TABLE oauth_provider_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY oauth_tokens_tenant_isolation ON oauth_provider_tokens
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
