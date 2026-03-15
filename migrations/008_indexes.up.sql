CREATE INDEX IF NOT EXISTS idx_emails_account_folder ON emails(tenant_id, account_id, folder, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_emails_starred ON emails(tenant_id, account_id) WHERE is_starred = true;
CREATE INDEX IF NOT EXISTS idx_categories_category ON email_categories(tenant_id, category);
CREATE INDEX IF NOT EXISTS idx_drafts_status ON draft_replies(tenant_id, status) WHERE status = 'generated';
