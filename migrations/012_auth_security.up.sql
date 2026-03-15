CREATE TABLE auth_lockouts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identifier      TEXT NOT NULL,
    identifier_type TEXT NOT NULL CHECK(identifier_type IN ('email','ip')),
    failed_attempts INT DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ DEFAULT now(),
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(identifier, identifier_type)
);

CREATE INDEX idx_lockouts_locked ON auth_lockouts(identifier, identifier_type)
    WHERE locked_until IS NOT NULL;
