DROP POLICY IF EXISTS task_time_entries_tenant_isolation ON task_time_entries;
DROP TABLE IF EXISTS task_time_entries;
DROP POLICY IF EXISTS task_board_columns_tenant_isolation ON task_board_columns;
DROP TABLE IF EXISTS task_board_columns;
DROP POLICY IF EXISTS task_dismissed_feedback_tenant_isolation ON task_dismissed_feedback;
DROP TABLE IF EXISTS task_dismissed_feedback;
