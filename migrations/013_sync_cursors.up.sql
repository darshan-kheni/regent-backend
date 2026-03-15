CREATE TABLE sync_cursors (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id      UUID NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL CHECK(provider IN ('imap','gmail')),
    last_uid        BIGINT CHECK(last_uid > 0),
    last_history_id TEXT,
    sync_state      TEXT NOT NULL DEFAULT 'pending'
                    CHECK(sync_state IN ('pending','syncing','active','error','paused')),
    progress_pct    INT DEFAULT 0 CHECK(progress_pct BETWEEN 0 AND 100),
    total_messages  INT,
    synced_messages INT DEFAULT 0,
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(account_id, provider)
);

CREATE INDEX idx_sync_cursors_account ON sync_cursors(tenant_id, account_id);
CREATE INDEX idx_sync_cursors_state ON sync_cursors(sync_state) WHERE sync_state IN ('syncing','error');

ALTER TABLE sync_cursors ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_cursors_tenant_isolation ON sync_cursors
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TRIGGER set_sync_cursors_updated_at BEFORE UPDATE ON sync_cursors
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
