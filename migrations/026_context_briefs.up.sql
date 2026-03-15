CREATE TABLE context_briefs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL REFERENCES tenants(id),
  title           TEXT NOT NULL,
  scope           TEXT NOT NULL,
  text            TEXT NOT NULL,
  keywords        JSONB DEFAULT '[]',
  expires_at      TIMESTAMPTZ,
  embedding       vector(768),
  created_at      TIMESTAMPTZ DEFAULT now()
);
-- NOTE: No is_active generated column — now() is STABLE, not IMMUTABLE.
-- Filter at query time: WHERE expires_at IS NULL OR expires_at > now()
CREATE INDEX idx_briefs_user_expires ON context_briefs(user_id, expires_at);
CREATE INDEX idx_briefs_keywords ON context_briefs USING gin(keywords);
CREATE INDEX idx_briefs_tenant ON context_briefs(tenant_id, user_id);

ALTER TABLE context_briefs ENABLE ROW LEVEL SECURITY;
CREATE POLICY briefs_tenant ON context_briefs
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
