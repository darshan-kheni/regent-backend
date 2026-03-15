CREATE TABLE stress_indicators (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  date       DATE NOT NULL,
  metric     TEXT NOT NULL CHECK (metric IN (
    'response_time_trend', 'late_night_activity',
    'email_volume', 'tone_consistency', 'weekend_boundary'
  )),
  value      TEXT NOT NULL,
  delta      TEXT,
  status     TEXT NOT NULL CHECK (status IN ('ok', 'warn', 'critical')),
  detail     TEXT,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, date, metric)
);

CREATE INDEX idx_stress_user_date ON stress_indicators (user_id, date DESC);
CREATE INDEX idx_stress_tenant ON stress_indicators (tenant_id, created_at);

ALTER TABLE stress_indicators ENABLE ROW LEVEL SECURITY;
CREATE POLICY stress_indicators_tenant_isolation ON stress_indicators
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
