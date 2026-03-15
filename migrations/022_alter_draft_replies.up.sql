ALTER TABLE draft_replies
  ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id),
  ADD COLUMN IF NOT EXISTS content TEXT,
  ADD COLUMN IF NOT EXISTS prompt_version TEXT;
UPDATE draft_replies SET content = body WHERE content IS NULL;
ALTER TABLE draft_replies DROP CONSTRAINT IF EXISTS draft_replies_status_check;
ALTER TABLE draft_replies ADD CONSTRAINT draft_replies_status_check
  CHECK(status IN ('generated','approved','edited','sent','discarded'));
