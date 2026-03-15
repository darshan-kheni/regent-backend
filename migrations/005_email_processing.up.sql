CREATE TABLE email_categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    email_id    UUID NOT NULL REFERENCES emails(id) ON DELETE CASCADE,
    category    TEXT NOT NULL,
    confidence  FLOAT NOT NULL,
    model_used  TEXT,
    created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_categories_email ON email_categories(email_id);
CREATE INDEX idx_categories_tenant ON email_categories(tenant_id, created_at DESC);
ALTER TABLE email_categories ENABLE ROW LEVEL SECURITY;
CREATE POLICY categories_tenant_isolation ON email_categories
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TABLE email_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    email_id    UUID NOT NULL REFERENCES emails(id) ON DELETE CASCADE,
    summary     TEXT NOT NULL,
    model_used  TEXT,
    created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_summaries_email ON email_summaries(email_id);
ALTER TABLE email_summaries ENABLE ROW LEVEL SECURITY;
CREATE POLICY summaries_tenant_isolation ON email_summaries
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE TABLE draft_replies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    email_id    UUID NOT NULL REFERENCES emails(id) ON DELETE CASCADE,
    body        TEXT NOT NULL,
    variant     TEXT DEFAULT 'professional',
    model_used  TEXT,
    is_premium  BOOLEAN DEFAULT false,
    confidence  FLOAT,
    status      TEXT DEFAULT 'generated'
                CHECK(status IN ('generated','edited','sent','discarded')),
    created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_drafts_email ON draft_replies(email_id);
CREATE INDEX idx_drafts_tenant ON draft_replies(tenant_id, created_at DESC);
ALTER TABLE draft_replies ENABLE ROW LEVEL SECURITY;
CREATE POLICY drafts_tenant_isolation ON draft_replies
    FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
