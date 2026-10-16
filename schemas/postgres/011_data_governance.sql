-- Per-project data rules: PII scrubbing at ingest and retention periods.
-- Projects without a row use the defaults in internal/governance.
CREATE TABLE IF NOT EXISTS project_data_settings (
    project_id              BIGINT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    scrub_emails            BOOLEAN NOT NULL DEFAULT false,
    scrub_cards             BOOLEAN NOT NULL DEFAULT true,
    scrub_ips               BOOLEAN NOT NULL DEFAULT false,
    scrub_keys              TEXT[] NOT NULL DEFAULT '{}',
    scrub_patterns          TEXT[] NOT NULL DEFAULT '{}',
    retention_events_days     INTEGER NOT NULL DEFAULT 30,
    retention_traces_days     INTEGER NOT NULL DEFAULT 7,
    retention_logs_days       INTEGER NOT NULL DEFAULT 3,
    retention_errors_days     INTEGER NOT NULL DEFAULT 90,
    retention_replays_days    INTEGER NOT NULL DEFAULT 30,
    retention_sourcemaps_days INTEGER NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Requests to delete all data of one person (every linked id).
CREATE TABLE IF NOT EXISTS deletion_requests (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    distinct_id  TEXT NOT NULL,
    person_key   TEXT NOT NULL,
    distinct_ids TEXT[] NOT NULL DEFAULT '{}',
    reason       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    requested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    result       JSONB NOT NULL DEFAULT '{}',
    error        TEXT NOT NULL DEFAULT '',
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_deletion_requests_project ON deletion_requests (project_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_deletion_requests_open ON deletion_requests (status) WHERE status IN ('pending', 'running');
