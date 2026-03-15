CREATE TABLE automation_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    trigger_type TEXT NOT NULL
                CHECK(trigger_type IN ('email_received','email_categorized','schedule','manual')),
    conditions  JSONB NOT NULL DEFAULT '{}',
    actions     JSONB NOT NULL DEFAULT '{}',
    is_active   BOOLEAN DEFAULT true,
    priority    INT DEFAULT 0,
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_rules_tenant ON automation_rules(tenant_id, user_id);
CREATE INDEX idx_rules_active ON automation_rules(tenant_id)
    WHERE is_active = true;
ALTER TABLE automation_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY rules_tenant_isolation ON automation_rules
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
