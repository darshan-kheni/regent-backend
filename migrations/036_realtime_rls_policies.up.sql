-- Migration 036: Add auth.uid()-based RLS policies for Supabase Realtime
-- These supplement the existing tenant_id-based policies used by the Go backend.
-- Supabase Realtime uses auth.uid() from JWT, not the app.tenant_id GUC.
-- Chain: auth.uid() → users.auth_id → users.id → table's user_id

-- emails: direct user_id column
CREATE POLICY emails_realtime_select ON emails
  FOR SELECT USING (
    user_id IN (
      SELECT id FROM users WHERE auth_id = auth.uid()
    )
  );

-- draft_replies: linked via email_id → emails.user_id
CREATE POLICY draft_replies_realtime_select ON draft_replies
  FOR SELECT USING (
    email_id IN (
      SELECT id FROM emails WHERE user_id IN (
        SELECT id FROM users WHERE auth_id = auth.uid()
      )
    )
  );

-- token_usage_daily: direct user_id column
CREATE POLICY token_usage_daily_realtime_select ON token_usage_daily
  FOR SELECT USING (
    user_id IN (
      SELECT id FROM users WHERE auth_id = auth.uid()
    )
  );

-- ai_audit_log: direct user_id column
CREATE POLICY ai_audit_log_realtime_select ON ai_audit_log
  FOR SELECT USING (
    user_id IN (
      SELECT id FROM users WHERE auth_id = auth.uid()
    )
  );

-- notification_log: direct user_id column
CREATE POLICY notification_log_realtime_select ON notification_log
  FOR SELECT USING (
    user_id IN (
      SELECT id FROM users WHERE auth_id = auth.uid()
    )
  );

-- Add tables to Realtime publication
-- Using DO block to handle "already added" gracefully
DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE emails;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE draft_replies;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE token_usage_daily;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE ai_audit_log;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE notification_log;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
