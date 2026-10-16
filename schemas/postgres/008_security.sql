-- Idle session expiry: sessions unused for longer than the idle timeout are
-- rejected even before their absolute expires_at.
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Browser origins allowed to send data with this project's ingest keys.
-- Empty means any origin.
ALTER TABLE projects ADD COLUMN IF NOT EXISTS allowed_origins TEXT[] NOT NULL DEFAULT '{}';

-- Security-relevant actions: logins, key and token changes, invites, role
-- changes. org_id is NULL for events that belong to a user (e.g. logins).
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    org_id      BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    project_id  BIGINT REFERENCES projects(id) ON DELETE SET NULL,
    actor_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_email TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    details     JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_audit_log_org ON audit_log (org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log (actor_id, created_at DESC);
