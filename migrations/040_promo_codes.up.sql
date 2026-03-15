-- Phase 7: Promo codes and redemptions
CREATE TABLE promo_codes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code            TEXT UNIQUE NOT NULL,
    type            TEXT NOT NULL CHECK (type IN ('discount','trial')),
    discount_percent INT CHECK (discount_percent BETWEEN 1 AND 100),
    trial_days      INT CHECK (trial_days BETWEEN 1 AND 365),
    plan            TEXT NOT NULL CHECK (plan IN ('attache','privy_council','estate')),
    max_uses        INT,
    current_uses    INT DEFAULT 0,
    valid_from      TIMESTAMPTZ DEFAULT now(),
    valid_until     TIMESTAMPTZ,
    active          BOOLEAN DEFAULT true,
    created_by      UUID,
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE promo_redemptions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    code_id         UUID NOT NULL REFERENCES promo_codes(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    redeemed_at     TIMESTAMPTZ DEFAULT now(),
    applied_plan    TEXT NOT NULL,
    original_price  INT,
    discounted_price INT,
    trial_end_date  TIMESTAMPTZ,
    UNIQUE(code_id, user_id)
);

ALTER TABLE promo_redemptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY promo_redemptions_isolation ON promo_redemptions
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE INDEX idx_promo_redemptions_tenant ON promo_redemptions (tenant_id);
CREATE INDEX idx_promo_redemptions_trial ON promo_redemptions (trial_end_date)
    WHERE trial_end_date IS NOT NULL;

-- Seed promo codes
INSERT INTO promo_codes (code, type, discount_percent, plan, max_uses) VALUES
    ('BETA50', 'discount', 50, 'attache', 100);
INSERT INTO promo_codes (code, type, trial_days, plan, max_uses) VALUES
    ('LAUNCH30', 'trial', 30, 'privy_council', 500);
