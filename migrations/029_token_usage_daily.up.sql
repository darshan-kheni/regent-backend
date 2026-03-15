CREATE TABLE token_usage_daily (
  id            BIGSERIAL PRIMARY KEY,
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id     UUID NOT NULL REFERENCES tenants(id),
  date          DATE NOT NULL,
  service       TEXT NOT NULL,
  model_used    TEXT NOT NULL,
  total_tokens  BIGINT DEFAULT 0,
  total_calls   INT DEFAULT 0,
  total_latency_ms BIGINT DEFAULT 0,
  cache_hits    INT DEFAULT 0,
  premium_calls INT DEFAULT 0,
  created_at    TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, date, service, model_used)
);
CREATE INDEX idx_usage_daily_tenant ON token_usage_daily(tenant_id, date);

ALTER TABLE token_usage_daily ENABLE ROW LEVEL SECURITY;
CREATE POLICY usage_daily_tenant ON token_usage_daily
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE MATERIALIZED VIEW token_usage_summary AS
SELECT
  user_id,
  tenant_id,
  date_trunc('week', date) AS week,
  SUM(total_tokens) AS weekly_tokens,
  SUM(total_calls) AS weekly_calls,
  SUM(premium_calls) AS weekly_premium,
  SUM(total_latency_ms) / NULLIF(SUM(total_calls), 0) AS avg_latency_ms
FROM token_usage_daily
GROUP BY 1, 2, 3;

-- Required for REFRESH MATERIALIZED VIEW CONCURRENTLY
CREATE UNIQUE INDEX idx_usage_summary_unique ON token_usage_summary(user_id, tenant_id, week);
