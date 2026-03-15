CREATE TABLE wellness_reports (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  week_start  DATE NOT NULL,
  report_text TEXT NOT NULL,
  model_used  TEXT,
  tokens_used INT,
  insights    JSONB,
  created_at  TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, week_start)
);

CREATE INDEX idx_wellness_user ON wellness_reports (user_id, week_start DESC);
CREATE INDEX idx_wellness_tenant ON wellness_reports (tenant_id, created_at);

ALTER TABLE wellness_reports ENABLE ROW LEVEL SECURITY;
CREATE POLICY wellness_reports_tenant_isolation ON wellness_reports
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
