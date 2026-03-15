ALTER TABLE email_categories ALTER COLUMN primary_category DROP NOT NULL;
ALTER TABLE email_categories DROP COLUMN IF EXISTS secondary_category;
ALTER TABLE email_categories DROP COLUMN IF EXISTS primary_category;
