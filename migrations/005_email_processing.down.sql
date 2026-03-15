DROP POLICY IF EXISTS drafts_tenant_isolation ON draft_replies;
DROP TABLE IF EXISTS draft_replies;
DROP POLICY IF EXISTS summaries_tenant_isolation ON email_summaries;
DROP TABLE IF EXISTS email_summaries;
DROP POLICY IF EXISTS categories_tenant_isolation ON email_categories;
DROP TABLE IF EXISTS email_categories;
