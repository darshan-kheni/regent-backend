CREATE TABLE user_rules (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL,
  scope           TEXT NOT NULL CHECK(scope IN (
    'email','calendar','tasks','contacts','travel','all'
  )),
  type            TEXT NOT NULL CHECK(type IN (
    'tone','reply_template','auto_action','priority_rule','context'
  )),
  text            TEXT NOT NULL CHECK(char_length(text) <= 500),
  contact_filter  TEXT,
  active          BOOLEAN DEFAULT true,
  priority        INT DEFAULT 0,
  created_at      TIMESTAMPTZ DEFAULT now(),
  updated_at      TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_rules_active ON user_rules(user_id, scope, active) WHERE active = true;

ALTER TABLE user_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY rules_tenant ON user_rules
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
