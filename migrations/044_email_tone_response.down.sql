DROP INDEX IF EXISTS idx_emails_response_time;
DROP INDEX IF EXISTS idx_emails_tone;
ALTER TABLE emails DROP COLUMN IF EXISTS response_time_minutes;
ALTER TABLE emails DROP COLUMN IF EXISTS tone_classification;
