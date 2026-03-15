CREATE TABLE auth_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    session_id      TEXT NOT NULL,
    device_name     TEXT,
    user_agent      TEXT,
    ip_address      INET,
    last_active_at  TIMESTAMPTZ DEFAULT now(),
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(session_id)
);

CREATE INDEX idx_sessions_user ON auth_sessions(user_id, last_active_at DESC);
ALTER TABLE auth_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY sessions_tenant_isolation ON auth_sessions
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
