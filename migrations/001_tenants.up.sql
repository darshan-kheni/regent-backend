CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT UNIQUE NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free'
                CHECK(plan IN ('free','attache','privy_council','estate')),
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK(status IN ('active','suspended','cancelled')),
    settings    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_self_access ON tenants
    FOR ALL USING (id = current_setting('app.tenant_id', true)::uuid);
