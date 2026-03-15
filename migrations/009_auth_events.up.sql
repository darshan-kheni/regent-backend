CREATE TABLE auth_events (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID REFERENCES users(id),
    tenant_id   UUID REFERENCES tenants(id),
    event_type  TEXT NOT NULL CHECK(event_type IN (
        'signup', 'login', 'logout', 'login_failed',
        'password_reset_request', 'password_changed',
        'oauth_connect', 'oauth_disconnect',
        'session_revoked', 'account_locked', 'account_unlocked',
        'mfa_enabled', 'mfa_disabled'
    )),
    provider    TEXT,
    ip_address  INET,
    user_agent  TEXT,
    metadata    JSONB DEFAULT '{}',
    success     BOOLEAN DEFAULT true,
    created_at  TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_auth_events_user ON auth_events(user_id, created_at DESC);
CREATE INDEX idx_auth_events_type ON auth_events(event_type, created_at DESC);
CREATE INDEX idx_auth_events_failed ON auth_events(ip_address, created_at) WHERE NOT success;
ALTER TABLE auth_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY auth_events_tenant_isolation ON auth_events
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
