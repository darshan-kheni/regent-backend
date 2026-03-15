-- Phase 7: Idempotent webhook event deduplication
CREATE TABLE webhook_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stripe_event_id TEXT UNIQUE NOT NULL,
    event_type      TEXT NOT NULL,
    processed_at    TIMESTAMPTZ DEFAULT now(),
    payload         JSONB
);

CREATE INDEX idx_webhook_stripe_id ON webhook_events (stripe_event_id);
