ALTER TABLE emails ADD COLUMN IF NOT EXISTS tone_classification TEXT;
ALTER TABLE emails ADD COLUMN IF NOT EXISTS response_time_minutes NUMERIC(8,2);

CREATE INDEX idx_emails_tone ON emails (tenant_id, user_id, tone_classification)
  WHERE tone_classification IS NOT NULL;
CREATE INDEX idx_emails_response_time ON emails (tenant_id, user_id, response_time_minutes)
  WHERE response_time_minutes IS NOT NULL;
