-- Allow 'pending' and 'rejected' statuses for draft replies
ALTER TABLE draft_replies DROP CONSTRAINT IF EXISTS draft_replies_status_check;
ALTER TABLE draft_replies ADD CONSTRAINT draft_replies_status_check
  CHECK(status IN ('pending','generated','approved','rejected','edited','sent','discarded'));

-- Update existing 'generated' drafts to 'pending' so they appear in reply queue
UPDATE draft_replies SET status = 'pending' WHERE status = 'generated';

-- Update the partial index to match new status
DROP INDEX IF EXISTS idx_drafts_status;
CREATE INDEX idx_drafts_status ON draft_replies(tenant_id, status) WHERE status = 'pending';
