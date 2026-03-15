CREATE TABLE prompt_versions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_type   TEXT NOT NULL,
  version     INT NOT NULL,
  template    TEXT NOT NULL,
  is_active   BOOLEAN DEFAULT true,
  created_at  TIMESTAMPTZ DEFAULT now(),
  UNIQUE(task_type, version)
);
