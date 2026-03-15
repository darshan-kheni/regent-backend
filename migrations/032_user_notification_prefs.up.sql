CREATE TABLE user_notification_prefs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    sms_enabled BOOLEAN NOT NULL DEFAULT false,
    whatsapp_enabled BOOLEAN NOT NULL DEFAULT false,
    signal_enabled BOOLEAN NOT NULL DEFAULT false,
    push_enabled BOOLEAN NOT NULL DEFAULT true,
    digest_enabled BOOLEAN NOT NULL DEFAULT true,
    primary_channel TEXT NOT NULL DEFAULT 'push'
        CHECK (primary_channel IN ('sms','whatsapp','signal','push','email_digest')),
    sms_phone TEXT,
    whatsapp_phone TEXT,
    signal_id TEXT,
    digest_frequency TEXT NOT NULL DEFAULT 'daily'
        CHECK (digest_frequency IN ('daily','twice_daily','weekly','off')),
    digest_time TIME NOT NULL DEFAULT '07:00',
    digest_timezone TEXT NOT NULL DEFAULT 'UTC',
    quiet_start TIME,
    quiet_end TIME,
    quiet_timezone TEXT NOT NULL DEFAULT 'UTC',
    vip_breaks_quiet BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE user_notification_prefs ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_notification_prefs_tenant_isolation ON user_notification_prefs
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
