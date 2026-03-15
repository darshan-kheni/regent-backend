CREATE TABLE meeting_briefs (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id         UUID NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
  user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  brief_text       TEXT NOT NULL,
  model_used       TEXT,
  tokens_used      INT,
  attendee_context JSONB,
  agenda_detected  TEXT,
  generated_at     TIMESTAMPTZ DEFAULT now(),
  UNIQUE(event_id, user_id)
);

CREATE INDEX idx_meeting_briefs_event ON meeting_briefs (event_id);
CREATE INDEX idx_meeting_briefs_tenant ON meeting_briefs (tenant_id, generated_at);

ALTER TABLE meeting_briefs ENABLE ROW LEVEL SECURITY;
CREATE POLICY meeting_briefs_tenant_isolation ON meeting_briefs
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TABLE meeting_notes (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id       UUID NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  notes          TEXT,
  outcome        TEXT CHECK (outcome IN ('productive', 'neutral', 'needs_followup', 'cancelled')),
  followup_items JSONB,
  created_at     TIMESTAMPTZ DEFAULT now(),
  UNIQUE(event_id, user_id)
);

CREATE INDEX idx_meeting_notes_user ON meeting_notes (user_id, created_at DESC);
CREATE INDEX idx_meeting_notes_tenant ON meeting_notes (tenant_id, created_at);

ALTER TABLE meeting_notes ENABLE ROW LEVEL SECURITY;
CREATE POLICY meeting_notes_tenant_isolation ON meeting_notes
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
