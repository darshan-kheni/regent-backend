CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    auth_id       UUID UNIQUE,
    email         TEXT NOT NULL,
    full_name     TEXT,
    avatar_url    TEXT,
    role          TEXT DEFAULT 'owner'
                  CHECK(role IN ('owner','admin','member')),
    timezone      TEXT DEFAULT 'UTC',
    language      TEXT DEFAULT 'en',
    status        TEXT DEFAULT 'active'
                  CHECK(status IN ('active','inactive','deleted')),
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(tenant_id, email)
);
CREATE INDEX idx_users_tenant ON users(tenant_id);
CREATE INDEX idx_users_auth ON users(auth_id);
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_tenant_isolation ON users
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
