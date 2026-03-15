ALTER TABLE email_summaries
  ADD COLUMN IF NOT EXISTS headline TEXT,
  ADD COLUMN IF NOT EXISTS key_points JSONB,
  ADD COLUMN IF NOT EXISTS action_required BOOLEAN DEFAULT false;
UPDATE email_summaries SET headline = split_part(summary, '.', 1) WHERE headline IS NULL AND summary IS NOT NULL;
