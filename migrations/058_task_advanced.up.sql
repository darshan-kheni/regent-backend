-- Task dismissed feedback for AI feedback loop
CREATE TABLE task_dismissed_feedback (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_title      TEXT NOT NULL,
    task_description TEXT,
    source_sender   TEXT,
    source_subject  TEXT,
    task_type       TEXT,
    dismissed_at    TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_dismissed_feedback_user ON task_dismissed_feedback (user_id, dismissed_at);

ALTER TABLE task_dismissed_feedback ENABLE ROW LEVEL SECURITY;
CREATE POLICY task_dismissed_feedback_tenant_isolation ON task_dismissed_feedback
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Kanban column customization
CREATE TABLE task_board_columns (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    column_key  TEXT NOT NULL,
    label       TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#3B82F6',
    position    INT NOT NULL DEFAULT 0,
    is_default  BOOLEAN DEFAULT false,
    created_at  TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id, column_key)
);

ALTER TABLE task_board_columns ENABLE ROW LEVEL SECURITY;
CREATE POLICY task_board_columns_tenant_isolation ON task_board_columns
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Time tracking entries
CREATE TABLE task_time_entries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    started_at  TIMESTAMPTZ NOT NULL,
    ended_at    TIMESTAMPTZ,
    duration_min INT,
    created_at  TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_time_entries_task ON task_time_entries (task_id);
CREATE INDEX idx_time_entries_user ON task_time_entries (user_id, started_at);

ALTER TABLE task_time_entries ENABLE ROW LEVEL SECURITY;
CREATE POLICY task_time_entries_tenant_isolation ON task_time_entries
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
