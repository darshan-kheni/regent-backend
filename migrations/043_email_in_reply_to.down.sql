DROP INDEX IF EXISTS idx_emails_in_reply_to;
ALTER TABLE emails DROP COLUMN IF EXISTS in_reply_to;
