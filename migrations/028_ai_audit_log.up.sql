CREATE TABLE ai_audit_log (
  id            BIGSERIAL,
  user_id       UUID NOT NULL,
  tenant_id     UUID NOT NULL,
  email_id      UUID,
  task_type     TEXT NOT NULL CHECK(task_type IN (
    'categorize','prioritize','summarize','draft_reply',
    'premium_draft','embedding','preference_synthesis','behavior_analysis'
  )),
  model_used    TEXT NOT NULL,
  model_version TEXT,
  prompt_version TEXT,
  input_hash    TEXT,
  tokens_in     INT NOT NULL,
  tokens_out    INT NOT NULL,
  total_tokens  INT GENERATED ALWAYS AS (tokens_in + tokens_out) STORED,
  confidence    NUMERIC(3,2),
  latency_ms    INT,
  cache_hit     BOOLEAN DEFAULT false,
  raw_output    JSONB,
  parsed_output JSONB,
  created_at    TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Pre-create 6 months of partitions
CREATE TABLE ai_audit_log_2026_01 PARTITION OF ai_audit_log
  FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE ai_audit_log_2026_02 PARTITION OF ai_audit_log
  FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE ai_audit_log_2026_03 PARTITION OF ai_audit_log
  FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE ai_audit_log_2026_04 PARTITION OF ai_audit_log
  FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE ai_audit_log_2026_05 PARTITION OF ai_audit_log
  FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE ai_audit_log_2026_06 PARTITION OF ai_audit_log
  FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

-- Default partition as safety net
CREATE TABLE ai_audit_log_default PARTITION OF ai_audit_log DEFAULT;

CREATE INDEX idx_audit_user_date ON ai_audit_log(user_id, created_at DESC);
CREATE INDEX idx_audit_task ON ai_audit_log(task_type, created_at DESC);
CREATE INDEX idx_audit_tenant_date ON ai_audit_log(tenant_id, created_at DESC);

ALTER TABLE ai_audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY ai_audit_log_tenant ON ai_audit_log
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
