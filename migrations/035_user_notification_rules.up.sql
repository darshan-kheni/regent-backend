CREATE TABLE user_notification_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rule_type TEXT NOT NULL CHECK (rule_type IN ('vip','sender','keyword','category','time')),
    condition JSONB NOT NULL DEFAULT '{}',
    action TEXT NOT NULL DEFAULT 'normal'
        CHECK (action IN ('critical','high','normal','suppress')),
    priority INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_rules_user_active
    ON user_notification_rules(user_id) WHERE active = true;

ALTER TABLE user_notification_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_notification_rules_tenant_isolation ON user_notification_rules
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
