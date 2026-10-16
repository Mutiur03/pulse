-- Custom dashboards: a named grid of saved queries (widgets) in a project.
-- Private dashboards are visible to their creator only; shared ones to
-- everyone who can read the project (its organization's members).
CREATE TABLE IF NOT EXISTS dashboards (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    shared      BOOLEAN NOT NULL DEFAULT false,
    widgets     JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_dashboards_project ON dashboards (project_id, updated_at DESC);
