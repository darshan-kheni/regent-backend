CREATE TABLE behavior_profiles (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  ai_understanding_score INT CHECK (ai_understanding_score BETWEEN 0 AND 100),
  wlb_score              INT CHECK (wlb_score BETWEEN 0 AND 100),
  calibration            JSONB DEFAULT '{}',
  last_computed          TIMESTAMPTZ,
  last_wlb_alert         TIMESTAMPTZ,
  created_at             TIMESTAMPTZ DEFAULT now(),
  updated_at             TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id)
);

-- UNIQUE(user_id) already creates implicit index; use (tenant_id, created_at) per CLAUDE.md
CREATE INDEX idx_behavior_profiles_tenant ON behavior_profiles (tenant_id, created_at);

ALTER TABLE behavior_profiles ENABLE ROW LEVEL SECURITY;
CREATE POLICY behavior_profiles_tenant_isolation ON behavior_profiles
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
