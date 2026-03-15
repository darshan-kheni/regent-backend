DROP POLICY IF EXISTS calendar_sync_state_tenant_isolation ON calendar_sync_state;
DROP TABLE IF EXISTS calendar_sync_state;
DROP POLICY IF EXISTS calendar_events_tenant_isolation ON calendar_events;
DROP TABLE IF EXISTS calendar_events;
