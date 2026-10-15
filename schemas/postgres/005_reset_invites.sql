-- Email verification, password resets and organization invites.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;

-- Single-use tokens sent by email. Only a SHA-256 of the token is stored.
CREATE TABLE IF NOT EXISTS email_tokens (
    token_hash BYTEA PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('reset', 'verify')),
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_email_tokens_user ON email_tokens (user_id, kind);

CREATE TABLE IF NOT EXISTS org_invites (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member')),
    token_hash  BYTEA NOT NULL UNIQUE,
    invited_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_org_invites_org ON org_invites (org_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;
