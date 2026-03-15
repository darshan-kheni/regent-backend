CREATE TABLE user_prompt_config (
  user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tenant_id            UUID NOT NULL,
  personality_summary  TEXT,
  few_shot_examples    JSONB DEFAULT '[]',
  updated_at           TIMESTAMPTZ DEFAULT now()
);

ALTER TABLE user_prompt_config ENABLE ROW LEVEL SECURITY;
CREATE POLICY prompt_config_tenant ON user_prompt_config
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
