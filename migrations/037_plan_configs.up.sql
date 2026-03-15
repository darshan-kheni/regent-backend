-- Phase 7: Plan configuration with limits and features (richer than 007_billing.billing_plans)
CREATE TABLE plan_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_name       TEXT UNIQUE NOT NULL,
    stripe_product_id TEXT,
    stripe_price_id TEXT,
    price_cents     INT NOT NULL DEFAULT 0,
    limits          JSONB NOT NULL DEFAULT '{}',
    features        JSONB NOT NULL DEFAULT '[]',
    active          BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now()
);

INSERT INTO plan_configs (plan_name, price_cents, limits, features) VALUES
    ('free', 0,
     '{"max_accounts":2,"daily_tokens":50000,"emails_month":500}',
     '["email_fetch","email_send","categorize","prioritize"]'),
    ('attache', 9700,
     '{"max_accounts":10,"daily_tokens":500000,"emails_month":10000}',
     '["email_fetch","email_send","categorize","prioritize","summarize","draft_reply","tone_analysis","rag","push","email_digest","basic_behavior"]'),
    ('privy_council', 29700,
     '{"max_accounts":25,"daily_tokens":2000000,"emails_month":50000}',
     '["email_fetch","email_send","categorize","prioritize","summarize","draft_reply","tone_analysis","rag","push","email_digest","basic_behavior","premium_draft","all_channels","full_behavior","wellness","rules_unlimited","briefs"]'),
    ('estate', 69700,
     '{"max_accounts":0,"daily_tokens":0,"emails_month":0}',
     '["email_fetch","email_send","categorize","prioritize","summarize","draft_reply","tone_analysis","rag","push","email_digest","basic_behavior","premium_draft","all_channels","full_behavior","wellness","rules_unlimited","briefs","custom_models","dedicated_channels","coaching","quarterly","unlimited_everything"]');
