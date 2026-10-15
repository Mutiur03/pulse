-- Releases, their uploaded source maps, and upload tokens for CI.
CREATE TABLE IF NOT EXISTS releases (
    id         BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, version)
);

CREATE TABLE IF NOT EXISTS release_artifacts (
    id         BIGSERIAL PRIMARY KEY,
    release_id BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- Where the generated file is served, e.g. "~/assets/app.js.map"; "~"
    -- stands for any origin.
    name       TEXT NOT NULL,
    debug_id   UUID,
    object_key TEXT NOT NULL,
    size       BIGINT NOT NULL,
    sha256     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (release_id, name)
);
CREATE INDEX IF NOT EXISTS idx_release_artifacts_debug ON release_artifacts (project_id, debug_id) WHERE debug_id IS NOT NULL;

-- Upload tokens authenticate source map uploads. They are a separate type
-- from public ingest keys: secret, and only good for uploads to one project.
CREATE TABLE IF NOT EXISTS upload_tokens (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   BYTEA NOT NULL UNIQUE,
    hint         TEXT NOT NULL,
    created_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_upload_tokens_project ON upload_tokens (project_id);
