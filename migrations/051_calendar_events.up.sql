CREATE TABLE calendar_events (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  account_id        UUID NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
  provider          TEXT NOT NULL CHECK (provider IN ('google', 'microsoft')),
  provider_event_id TEXT NOT NULL,
  calendar_id       TEXT NOT NULL DEFAULT 'primary',
  title             TEXT,
  description       TEXT,
  start_time        TIMESTAMPTZ NOT NULL,
  end_time          TIMESTAMPTZ NOT NULL,
  time_zone         TEXT,
  location          TEXT,
  is_all_day        BOOLEAN DEFAULT false,
  status            TEXT CHECK (status IN ('confirmed', 'tentative', 'cancelled')),
  attendees         JSONB,
  recurrence_rule   TEXT,
  organizer_email   TEXT,
  is_online         BOOLEAN DEFAULT false,
  meeting_url       TEXT,
  briefed_at        TIMESTAMPTZ,
  last_synced       TIMESTAMPTZ DEFAULT now(),
  created_at        TIMESTAMPTZ DEFAULT now(),
  updated_at        TIMESTAMPTZ DEFAULT now(),
  UNIQUE(account_id, provider_event_id)
);

CREATE INDEX idx_calendar_events_user_time ON calendar_events (user_id, start_time, end_time);
CREATE INDEX idx_calendar_events_user_end ON calendar_events (user_id, end_time);
CREATE INDEX idx_calendar_events_upcoming ON calendar_events (user_id, start_time) WHERE status != 'cancelled';
CREATE INDEX idx_calendar_events_tenant ON calendar_events (tenant_id, created_at);
CREATE INDEX idx_calendar_events_briefing ON calendar_events (user_id, start_time) WHERE briefed_at IS NULL AND status != 'cancelled';

ALTER TABLE calendar_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY calendar_events_tenant_isolation ON calendar_events
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TABLE calendar_sync_state (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  account_id UUID NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
  provider   TEXT NOT NULL CHECK (provider IN ('google', 'microsoft')),
  sync_token TEXT,
  status     TEXT DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'error')),
  last_sync  TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(account_id, provider)
);

CREATE INDEX idx_calendar_sync_state_tenant ON calendar_sync_state (tenant_id);

ALTER TABLE calendar_sync_state ENABLE ROW LEVEL SECURITY;
CREATE POLICY calendar_sync_state_tenant_isolation ON calendar_sync_state
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
