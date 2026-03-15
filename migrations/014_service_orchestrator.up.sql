-- Migration 014: Service Orchestrator tables
-- Supports always-on per-user service bundles with health tracking and cron scheduling.

-- Ensure the updated_at trigger function exists (may already be present from earlier migrations).
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Tracks per-user service health: IMAP, AI, cron, briefing statuses + heartbeat.
CREATE TABLE user_service_status (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    imap_status     TEXT DEFAULT 'stopped' CHECK(imap_status IN ('stopped','starting','running','error','paused')),
    ai_status       TEXT DEFAULT 'stopped' CHECK(ai_status IN ('stopped','starting','running','error','paused')),
    cron_status     TEXT DEFAULT 'stopped' CHECK(cron_status IN ('stopped','starting','running','error','paused')),
    briefing_status TEXT DEFAULT 'stopped' CHECK(briefing_status IN ('stopped','starting','running','error','paused')),
    last_heartbeat  TIMESTAMPTZ,
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id)
);

CREATE INDEX idx_service_status_tenant ON user_service_status(tenant_id);
CREATE INDEX idx_service_status_heartbeat ON user_service_status(last_heartbeat)
    WHERE imap_status = 'running' OR ai_status = 'running' OR cron_status = 'running' OR briefing_status = 'running';

ALTER TABLE user_service_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY service_status_tenant_isolation ON user_service_status
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TRIGGER set_service_status_updated_at BEFORE UPDATE ON user_service_status
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Tracks per-user cron jobs: last/next run, duration, status.
CREATE TABLE cron_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_type      TEXT NOT NULL,
    last_run      TIMESTAMPTZ,
    next_run      TIMESTAMPTZ,
    duration_ms   BIGINT,
    status        TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed')),
    error_message TEXT,
    created_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id, job_type)
);

CREATE INDEX idx_cron_jobs_user ON cron_jobs(tenant_id, user_id);
CREATE INDEX idx_cron_jobs_next ON cron_jobs(status, next_run) WHERE status = 'pending';

ALTER TABLE cron_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY cron_jobs_tenant_isolation ON cron_jobs
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
