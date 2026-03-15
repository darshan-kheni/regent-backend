CREATE TABLE billing_plans (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT UNIQUE NOT NULL,
    name            TEXT NOT NULL,
    price_cents     INT NOT NULL DEFAULT 0,
    daily_token_limit INT NOT NULL DEFAULT 1000,
    max_accounts    INT NOT NULL DEFAULT 1,
    features        JSONB DEFAULT '{}',
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now()
);

INSERT INTO billing_plans (slug, name, price_cents, daily_token_limit, max_accounts) VALUES
    ('free', 'Free', 0, 50000, 1),
    ('attache', 'Attache', 9700, 500000, 10),
    ('privy_council', 'Privy Council', 29700, 2000000, 25),
    ('estate', 'Estate', 69700, 999999999, 999999);

CREATE TABLE subscriptions (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              UUID NOT NULL REFERENCES tenants(id),
    stripe_subscription_id TEXT UNIQUE,
    stripe_customer_id     TEXT NOT NULL,
    plan                   TEXT NOT NULL CHECK(plan IN ('free','attache','privy_council','estate')),
    status                 TEXT DEFAULT 'active'
                           CHECK(status IN ('active','trialing','past_due','cancelled','suspended')),
    current_period_start   TIMESTAMPTZ,
    current_period_end     TIMESTAMPTZ,
    daily_token_limit      INT NOT NULL DEFAULT 50000,
    max_accounts           INT NOT NULL DEFAULT 2,
    created_at             TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_sub_tenant ON subscriptions(tenant_id);
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY sub_tenant_isolation ON subscriptions
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TABLE usage_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    user_id     UUID NOT NULL,
    action      TEXT NOT NULL,
    tokens_used INT DEFAULT 0,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_usage_tenant ON usage_logs(tenant_id, created_at DESC);
ALTER TABLE usage_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY usage_tenant_isolation ON usage_logs
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
