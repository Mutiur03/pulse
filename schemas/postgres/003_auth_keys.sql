-- Users, login sessions, project membership and public ingest keys.
CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (lower(email));

-- Only a SHA-256 of the session token is stored; the token lives in the cookie.
CREATE TABLE IF NOT EXISTS user_sessions (
    token_hash  BYTEA PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    user_agent  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user ON user_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_expires ON user_sessions (expires_at);

CREATE TABLE IF NOT EXISTS project_members (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

-- Public ingest keys. They are meant to ship in browser bundles, so they only
-- allow writing data into one project; rotating revokes the previous key.
CREATE TABLE IF NOT EXISTS api_keys (
    id                 BIGSERIAL PRIMARY KEY,
    project_id         BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    public_key         TEXT NOT NULL UNIQUE,
    rate_limit_per_sec INT NOT NULL DEFAULT 500 CHECK (rate_limit_per_sec > 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_api_keys_project ON api_keys (project_id) WHERE revoked_at IS NULL;
