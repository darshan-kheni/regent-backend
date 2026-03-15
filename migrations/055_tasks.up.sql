CREATE TABLE tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email_id          UUID REFERENCES emails(id) ON DELETE SET NULL,
    title             TEXT NOT NULL,
    description       TEXT,
    type              TEXT DEFAULT 'explicit_request'
                      CHECK (type IN ('explicit_request','implicit_task','self_commitment','recurring')),
    status            TEXT DEFAULT 'to_do'
                      CHECK (status IN ('to_do','in_progress','waiting','done','dismissed')),
    priority          TEXT DEFAULT 'p2'
                      CHECK (priority IN ('p0','p1','p2','p3')),
    deadline          TIMESTAMPTZ,
    deadline_text     TEXT,
    needs_confirmation BOOLEAN DEFAULT false,
    assignee_email    TEXT,
    delegated_to      TEXT,
    delegated_at      TIMESTAMPTZ,
    confidence        NUMERIC(3,2),
    source_subject    TEXT,
    source_sender     TEXT,
    recurrence_rule   TEXT,
    next_recurrence   TIMESTAMPTZ,
    snoozed_until     TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    dismissed_at      TIMESTAMPTZ,
    calendar_event_id UUID REFERENCES calendar_events(id) ON DELETE SET NULL,
    time_tracked_min  INT DEFAULT 0,
    is_timing         BOOLEAN DEFAULT false,
    timing_started_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_tasks_user_status ON tasks (user_id, status)
    WHERE status NOT IN ('done', 'dismissed');
CREATE INDEX idx_tasks_user_deadline ON tasks (user_id, deadline)
    WHERE deadline IS NOT NULL;
CREATE INDEX idx_tasks_email ON tasks (email_id);
CREATE INDEX idx_tasks_tenant_created ON tasks (tenant_id, created_at);
CREATE INDEX idx_tasks_recurrence ON tasks (user_id, next_recurrence)
    WHERE recurrence_rule IS NOT NULL AND next_recurrence IS NOT NULL;

ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
CREATE POLICY tasks_tenant_isolation ON tasks
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
