CREATE TABLE preference_signals (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL,
  signal_type     TEXT NOT NULL CHECK(signal_type IN (
    'recategorize','summary_edit','reply_edit','priority_override','vip_assign'
  )),
  original_value  JSONB,
  corrected_value JSONB,
  context         JSONB,
  created_at      TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_signals_user_date ON preference_signals(user_id, created_at DESC);

ALTER TABLE preference_signals ENABLE ROW LEVEL SECURITY;
CREATE POLICY signals_tenant ON preference_signals
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
