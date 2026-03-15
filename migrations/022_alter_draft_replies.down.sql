ALTER TABLE draft_replies DROP CONSTRAINT IF EXISTS draft_replies_status_check;
ALTER TABLE draft_replies ADD CONSTRAINT draft_replies_status_check
  CHECK(status IN ('generated','edited','sent','discarded'));
ALTER TABLE draft_replies DROP COLUMN IF EXISTS prompt_version;
ALTER TABLE draft_replies DROP COLUMN IF EXISTS content;
ALTER TABLE draft_replies DROP COLUMN IF EXISTS user_id;
