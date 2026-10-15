-- Issue tracking: statuses, regression bookkeeping and a sample trace.
INSERT INTO projects (id, name, project_key) VALUES (1, 'Default', 'default')
ON CONFLICT (id) DO NOTHING;
SELECT setval('projects_id_seq', GREATEST((SELECT MAX(id) FROM projects), 1));

ALTER TABLE issues ALTER COLUMN status SET DEFAULT 'unresolved';
UPDATE issues SET status = 'unresolved' WHERE status = 'open';

ALTER TABLE issues ADD COLUMN IF NOT EXISTS exception_type  TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS culprit         TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS platform        TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS service_name    TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS sample_trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS sample_span_id  TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS resolved_at     TIMESTAMPTZ;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS times_regressed INT NOT NULL DEFAULT 0;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS last_regressed  TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'issues_status_check') THEN
    ALTER TABLE issues ADD CONSTRAINT issues_status_check CHECK (status IN ('unresolved', 'resolved', 'ignored'));
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_issues_project_last_seen ON issues(project_id, last_seen DESC);
