CREATE TABLE scheduling_requests (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email_id            UUID REFERENCES emails(id) ON DELETE SET NULL,
  confidence          NUMERIC(3,2) NOT NULL CHECK (confidence BETWEEN 0 AND 1),
  proposed_times      JSONB,
  duration_hint       INT,
  attendees           JSONB,
  location_preference TEXT CHECK (location_preference IN ('in_person', 'virtual', 'either')),
  urgency             TEXT CHECK (urgency IN ('low', 'medium', 'high')),
  status              TEXT DEFAULT 'detected' CHECK (status IN ('detected', 'suggested', 'accepted', 'declined', 'expired')),
  suggested_slots     JSONB,
  accepted_slot       JSONB,
  created_at          TIMESTAMPTZ DEFAULT now(),
  updated_at          TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_scheduling_requests_user_status ON scheduling_requests (user_id, status, created_at DESC);
CREATE INDEX idx_scheduling_requests_tenant ON scheduling_requests (tenant_id, created_at);

ALTER TABLE scheduling_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY scheduling_requests_tenant_isolation ON scheduling_requests
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
