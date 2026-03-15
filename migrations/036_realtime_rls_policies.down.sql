-- Rollback Migration 036: Remove Realtime RLS policies
DROP POLICY IF EXISTS emails_realtime_select ON emails;
DROP POLICY IF EXISTS draft_replies_realtime_select ON draft_replies;
DROP POLICY IF EXISTS token_usage_daily_realtime_select ON token_usage_daily;
DROP POLICY IF EXISTS ai_audit_log_realtime_select ON ai_audit_log;
DROP POLICY IF EXISTS notification_log_realtime_select ON notification_log;
