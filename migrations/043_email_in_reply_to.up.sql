ALTER TABLE emails ADD COLUMN IF NOT EXISTS in_reply_to TEXT;

-- Index includes tenant_id for RLS compatibility
CREATE INDEX idx_emails_in_reply_to ON emails (tenant_id, in_reply_to)
  WHERE in_reply_to IS NOT NULL;

-- Batched backfill from headers JSONB
DO $$
DECLARE
  batch_size INT := 5000;
  rows_updated INT;
BEGIN
  LOOP
    UPDATE emails
    SET in_reply_to = headers->>'In-Reply-To'
    WHERE id IN (
      SELECT id FROM emails
      WHERE headers->>'In-Reply-To' IS NOT NULL
        AND in_reply_to IS NULL
      LIMIT batch_size
    );
    GET DIAGNOSTICS rows_updated = ROW_COUNT;
    EXIT WHEN rows_updated = 0;
    PERFORM pg_sleep(0.1);
  END LOOP;
END $$;
