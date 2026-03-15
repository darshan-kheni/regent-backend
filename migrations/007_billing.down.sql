DROP POLICY IF EXISTS usage_tenant_isolation ON usage_logs;
DROP TABLE IF EXISTS usage_logs;
DROP POLICY IF EXISTS sub_tenant_isolation ON subscriptions;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS billing_plans;
