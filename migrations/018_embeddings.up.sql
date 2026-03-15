CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE embeddings (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id     UUID NOT NULL REFERENCES tenants(id),
  source_type   TEXT NOT NULL CHECK(source_type IN (
    'sent_email','correction','contact','calendar','task'
  )),
  source_id     UUID NOT NULL,
  content_preview TEXT,
  embedding     vector(768) NOT NULL,
  metadata      JSONB DEFAULT '{}',
  created_at    TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_embeddings_hnsw ON embeddings
  USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 200);
CREATE INDEX idx_embeddings_user ON embeddings(user_id, source_type);
CREATE INDEX idx_embeddings_tenant ON embeddings(tenant_id, created_at DESC);

CREATE OR REPLACE FUNCTION match_embeddings(
  query_embedding vector(768),
  match_user_id UUID,
  match_threshold FLOAT DEFAULT 0.65,
  match_count INT DEFAULT 5
) RETURNS TABLE (
  id UUID, source_type TEXT, source_id UUID,
  content_preview TEXT, metadata JSONB, similarity FLOAT
) LANGUAGE sql STABLE AS $$
  SELECT id, source_type, source_id, content_preview, metadata,
         1 - (embedding <=> query_embedding) AS similarity
  FROM embeddings
  WHERE user_id = match_user_id
    AND 1 - (embedding <=> query_embedding) > match_threshold
  ORDER BY embedding <=> query_embedding
  LIMIT match_count;
$$;

ALTER TABLE embeddings ENABLE ROW LEVEL SECURITY;
CREATE POLICY embeddings_tenant ON embeddings
  FOR ALL USING (tenant_id = current_setting('app.tenant_id', true)::uuid);
