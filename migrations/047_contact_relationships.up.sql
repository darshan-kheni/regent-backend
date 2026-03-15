CREATE TABLE contact_relationships (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id                 UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  contact_email             TEXT NOT NULL,
  contact_name              TEXT,
  interaction_count         INT DEFAULT 0,
  avg_response_time_minutes NUMERIC(8,2),
  dominant_tone             TEXT,
  sentiment_trend           TEXT CHECK (sentiment_trend IN ('up', 'down', 'stable')),
  interaction_frequency     TEXT,
  last_interaction          TIMESTAMPTZ,
  first_interaction         TIMESTAMPTZ,
  is_declining              BOOLEAN DEFAULT false,
  created_at                TIMESTAMPTZ DEFAULT now(),
  updated_at                TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, contact_email)
);

CREATE INDEX idx_contact_rel_user ON contact_relationships
  (user_id, interaction_count DESC);
CREATE INDEX idx_contact_rel_tenant ON contact_relationships (tenant_id, created_at);
CREATE INDEX idx_contact_rel_declining ON contact_relationships (user_id)
  WHERE is_declining = true;

ALTER TABLE contact_relationships ENABLE ROW LEVEL SECURITY;
CREATE POLICY contact_relationships_tenant_isolation ON contact_relationships
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
