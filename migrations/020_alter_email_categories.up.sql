BEGIN;
LOCK TABLE email_categories IN EXCLUSIVE MODE;
ALTER TABLE email_categories
  ADD COLUMN IF NOT EXISTS primary_category TEXT,
  ADD COLUMN IF NOT EXISTS secondary_category TEXT;
UPDATE email_categories SET primary_category = category WHERE primary_category IS NULL;
ALTER TABLE email_categories ALTER COLUMN primary_category SET NOT NULL;
COMMIT;
