CREATE TABLE benchmark_results (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_type   TEXT NOT NULL,
  model       TEXT NOT NULL,
  accuracy    NUMERIC(5,2),
  latency_p50 INT,
  latency_p99 INT,
  tokens_per_sec INT,
  tested_at   TIMESTAMPTZ DEFAULT now()
);
