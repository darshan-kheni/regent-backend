CREATE TABLE learned_patterns (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL,
  category        TEXT NOT NULL CHECK(category IN (
    'communication_style','priority_preferences',
    'schedule_patterns','reply_patterns','delegation_patterns'
  )),
  pattern_text    TEXT NOT NULL,
  confidence      INT CHECK(confidence BETWEEN 0 AND 100),
  evidence_count  INT DEFAULT 0,
  source_description TEXT,
  last_updated    TIMESTAMPTZ DEFAULT now(),
  created_at      TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_patterns_conf ON learned_patterns(user_id, category) WHERE confidence >= 70;

ALTER TABLE learned_patterns ENABLE ROW LEVEL SECURITY;
CREATE POLICY patterns_tenant ON learned_patterns
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
