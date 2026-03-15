-- Phase 7: Add billing columns to tenants table
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS stripe_customer_id TEXT;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS plan_started_at TIMESTAMPTZ;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS plan_renews_at TIMESTAMPTZ;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS grace_period_ends TIMESTAMPTZ;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS payment_status TEXT DEFAULT 'active'
    CHECK (payment_status IN ('active','past_due','suspended','canceled'));
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS failure_count INT DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tenants_stripe_customer ON tenants (stripe_customer_id)
    WHERE stripe_customer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tenants_grace_period ON tenants (grace_period_ends)
    WHERE grace_period_ends IS NOT NULL;
