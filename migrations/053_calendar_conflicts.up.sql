CREATE TABLE calendar_conflicts (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  event_a_id  UUID REFERENCES calendar_events(id) ON DELETE CASCADE,
  event_b_id  UUID REFERENCES calendar_events(id) ON DELETE CASCADE,
  type        TEXT NOT NULL CHECK (type IN ('hard', 'soft', 'preference')),
  severity    TEXT CHECK (severity IN ('critical', 'warn', 'info')),
  overlap_min INT,
  gap_min     INT,
  detail      TEXT,
  resolved    BOOLEAN DEFAULT false,
  created_at  TIMESTAMPTZ DEFAULT now(),
  UNIQUE(event_a_id, event_b_id)
);

-- Deduplicate preference conflicts (event_b_id IS NULL):
CREATE UNIQUE INDEX idx_calendar_conflicts_pref_dedup
  ON calendar_conflicts (event_a_id, type) WHERE event_b_id IS NULL;

CREATE INDEX idx_calendar_conflicts_user ON calendar_conflicts (user_id, created_at DESC) WHERE NOT resolved;
CREATE INDEX idx_calendar_conflicts_tenant ON calendar_conflicts (tenant_id, created_at);

ALTER TABLE calendar_conflicts ENABLE ROW LEVEL SECURITY;
CREATE POLICY calendar_conflicts_tenant_isolation ON calendar_conflicts
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TABLE calendar_preferences (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  preferred_start_hour INT DEFAULT 9,
  preferred_end_hour   INT DEFAULT 18,
  buffer_minutes       INT DEFAULT 15,
  no_meeting_days      JSONB DEFAULT '[]',
  focus_blocks         JSONB DEFAULT '[]',
  home_timezone        TEXT DEFAULT 'UTC',
  created_at           TIMESTAMPTZ DEFAULT now(),
  updated_at           TIMESTAMPTZ DEFAULT now()
);

ALTER TABLE calendar_preferences ENABLE ROW LEVEL SECURITY;
CREATE POLICY calendar_preferences_tenant_isolation ON calendar_preferences
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
