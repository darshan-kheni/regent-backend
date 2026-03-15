CREATE TABLE task_delegations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id               UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id               UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    delegated_to_email    TEXT NOT NULL,
    delegated_to_name     TEXT,
    delegation_email_id   UUID REFERENCES emails(id) ON DELETE SET NULL,
    status                TEXT DEFAULT 'pending'
                          CHECK (status IN ('pending','in_progress','completed','overdue')),
    follow_up_date        TIMESTAMPTZ,
    follow_up_count       INT DEFAULT 0,
    last_follow_up        TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_delegations_task ON task_delegations (task_id);
CREATE INDEX idx_delegations_pending ON task_delegations (follow_up_date)
    WHERE status = 'pending';
CREATE INDEX idx_delegations_tenant ON task_delegations (tenant_id, created_at);

ALTER TABLE task_delegations ENABLE ROW LEVEL SECURITY;
CREATE POLICY task_delegations_tenant_isolation ON task_delegations
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
