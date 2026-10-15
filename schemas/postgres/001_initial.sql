-- Postgres migrations for metadata

CREATE TABLE IF NOT EXISTS projects (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    project_key VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS persons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id BIGINT NOT NULL REFERENCES projects(id),
    properties JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_persons_project_id ON persons(project_id);

CREATE TABLE IF NOT EXISTS person_distinct_ids (
    project_id BIGINT NOT NULL REFERENCES projects(id),
    distinct_id VARCHAR(255) NOT NULL,
    person_id UUID NOT NULL REFERENCES persons(id),
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (project_id, distinct_id)
);

CREATE INDEX IF NOT EXISTS idx_person_distinct_ids_person_id ON person_distinct_ids(person_id);

CREATE TABLE IF NOT EXISTS issues (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id),
    fingerprint VARCHAR(255) NOT NULL,
    title TEXT NOT NULL,
    status VARCHAR(50) DEFAULT 'open',
    first_seen TIMESTAMP DEFAULT NOW(),
    last_seen TIMESTAMP DEFAULT NOW(),
    count BIGINT DEFAULT 1,
    UNIQUE(project_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_issues_project_id ON issues(project_id);
CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);
