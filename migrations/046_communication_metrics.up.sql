CREATE TABLE communication_metrics (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id                 UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  period_start              DATE NOT NULL,
  period_end                DATE NOT NULL,
  period_type               TEXT NOT NULL CHECK (period_type IN ('daily', 'weekly', 'monthly')),
  avg_response_time_minutes NUMERIC(8,2),
  avg_email_length_words    INT,
  emails_sent               INT DEFAULT 0,
  emails_received           INT DEFAULT 0,
  tone_distribution         JSONB,
  formality_distribution    JSONB,
  peak_hours                JSONB,
  active_hours_start        TIME,
  active_hours_end          TIME,
  after_hours_pct           NUMERIC(5,2),
  weekend_emails            INT DEFAULT 0,
  created_at                TIMESTAMPTZ DEFAULT now(),
  updated_at                TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, period_start, period_type)
);

CREATE INDEX idx_comm_metrics_user_period ON communication_metrics
  (user_id, period_start DESC, period_type);
CREATE INDEX idx_comm_metrics_tenant ON communication_metrics (tenant_id, created_at);

ALTER TABLE communication_metrics ENABLE ROW LEVEL SECURITY;
CREATE POLICY communication_metrics_tenant_isolation ON communication_metrics
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
