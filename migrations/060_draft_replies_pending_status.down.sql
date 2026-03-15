-- Revert status constraint
ALTER TABLE draft_replies DROP CONSTRAINT IF EXISTS draft_replies_status_check;
ALTER TABLE draft_replies ADD CONSTRAINT draft_replies_status_check
  CHECK(status IN ('generated','approved','edited','sent','discarded'));

UPDATE draft_replies SET status = 'generated' WHERE status = 'pending';
UPDATE draft_replies SET status = 'discarded' WHERE status = 'rejected';

DROP INDEX IF EXISTS idx_drafts_status;
CREATE INDEX idx_drafts_status ON draft_replies(tenant_id, status) WHERE status = 'generated';
