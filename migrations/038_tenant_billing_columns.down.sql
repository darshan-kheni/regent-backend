DROP INDEX IF EXISTS idx_tenants_grace_period;
DROP INDEX IF EXISTS idx_tenants_stripe_customer;
ALTER TABLE tenants DROP COLUMN IF EXISTS failure_count;
ALTER TABLE tenants DROP COLUMN IF EXISTS payment_status;
ALTER TABLE tenants DROP COLUMN IF EXISTS grace_period_ends;
ALTER TABLE tenants DROP COLUMN IF EXISTS plan_renews_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS plan_started_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS stripe_subscription_id;
ALTER TABLE tenants DROP COLUMN IF EXISTS stripe_customer_id;
