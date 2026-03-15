CREATE TABLE task_reminders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    reminder_type   TEXT NOT NULL
                    CHECK (reminder_type IN ('48h','24h','2h','overdue','follow_up')),
    scheduled_at    TIMESTAMPTZ NOT NULL,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(task_id, reminder_type)
);

CREATE INDEX idx_reminders_pending ON task_reminders (scheduled_at)
    WHERE sent_at IS NULL;
CREATE INDEX idx_reminders_tenant ON task_reminders (tenant_id, created_at);

ALTER TABLE task_reminders ENABLE ROW LEVEL SECURITY;
CREATE POLICY task_reminders_tenant_isolation ON task_reminders
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
