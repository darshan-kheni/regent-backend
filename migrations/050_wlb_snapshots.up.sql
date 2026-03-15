CREATE TABLE wlb_snapshots (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  date                DATE NOT NULL,
  score               INT NOT NULL CHECK (score BETWEEN 0 AND 100),
  after_hours_penalty NUMERIC(5,2) NOT NULL DEFAULT 0,
  weekend_penalty     NUMERIC(5,2) NOT NULL DEFAULT 0,
  boundary_penalty    NUMERIC(5,2) NOT NULL DEFAULT 0,
  volume_penalty      NUMERIC(5,2) NOT NULL DEFAULT 0,
  created_at          TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, date)
);

CREATE INDEX idx_wlb_snapshots_user_date ON wlb_snapshots (user_id, date DESC);
CREATE INDEX idx_wlb_snapshots_tenant ON wlb_snapshots (tenant_id, created_at);

ALTER TABLE wlb_snapshots ENABLE ROW LEVEL SECURITY;
CREATE POLICY wlb_snapshots_tenant_isolation ON wlb_snapshots
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
