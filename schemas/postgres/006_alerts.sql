-- Alert rules, notification channels and the history of rule states.
CREATE TABLE IF NOT EXISTS alert_channels (
    id         BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('email', 'webhook', 'slack')),
    target     TEXT NOT NULL,
    secret     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_channels_project ON alert_channels (project_id);

CREATE TABLE IF NOT EXISTS alert_rules (
    id                BIGSERIAL PRIMARY KEY,
    project_id        BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    metric            TEXT NOT NULL,
    comparison        TEXT NOT NULL DEFAULT 'gt' CHECK (comparison IN ('gt', 'gte', 'lt', 'lte')),
    threshold         DOUBLE PRECISION NOT NULL DEFAULT 0,
    window_seconds    INTEGER NOT NULL CHECK (window_seconds BETWEEN 60 AND 86400),
    interval_seconds  INTEGER NOT NULL CHECK (interval_seconds BETWEEN 10 AND 3600),
    service           TEXT NOT NULL DEFAULT '',
    route             TEXT NOT NULL DEFAULT '',
    environment       TEXT NOT NULL DEFAULT '',
    event_name        TEXT NOT NULL DEFAULT '',
    renotify_seconds  INTEGER NOT NULL DEFAULT 0,
    channel_ids       BIGINT[] NOT NULL DEFAULT '{}',
    enabled           BOOLEAN NOT NULL DEFAULT true,
    muted_until       TIMESTAMPTZ,
    state             TEXT NOT NULL DEFAULT 'ok' CHECK (state IN ('ok', 'alerting', 'no_data')),
    state_since       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_value        DOUBLE PRECISION,
    last_evaluated_at TIMESTAMPTZ,
    last_notified_at  TIMESTAMPTZ,
    last_triggered_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_rules_project ON alert_rules (project_id);
CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules (enabled, metric);

CREATE TABLE IF NOT EXISTS alert_history (
    id         BIGSERIAL PRIMARY KEY,
    rule_id    BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    state      TEXT NOT NULL,
    previous   TEXT NOT NULL,
    value      DOUBLE PRECISION,
    notice     TEXT NOT NULL DEFAULT '',
    -- Why a notice was not sent, e.g. muted or cooldown.
    suppressed TEXT NOT NULL DEFAULT '',
    -- Channels that failed, with the error.
    failures   TEXT NOT NULL DEFAULT '',
    issue_id   BIGINT,
    trace_id   TEXT NOT NULL DEFAULT '',
    message    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_alert_history_rule ON alert_history (rule_id, at DESC);
