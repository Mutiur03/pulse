-- Organizations own projects. Users sign up into their own organization and
-- create projects inside it.
CREATE TABLE IF NOT EXISTS organizations (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS org_members (
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_org_members_user ON org_members (user_id);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_projects_org ON projects (org_id);

-- project_key is a legacy slug; new projects get a random one.
ALTER TABLE projects ALTER COLUMN project_key SET DEFAULT ('p_' || md5(random()::text || clock_timestamp()::text));

-- Projects created before organizations existed move into one default
-- organization, and their members join it.
DO $$
DECLARE o BIGINT;
BEGIN
    IF EXISTS (SELECT 1 FROM projects WHERE org_id IS NULL) THEN
        INSERT INTO organizations (name) VALUES ('Default') RETURNING id INTO o;
        UPDATE projects SET org_id = o WHERE org_id IS NULL;
        INSERT INTO org_members (org_id, user_id, role)
        SELECT DISTINCT o, user_id, 'member' FROM project_members
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
