-- Local development seed, applied by the postgres-migrate service after the
-- migrations. Never use these credentials outside a local stack.
--   login:      demo@pulse.local / pulse-demo
--   ingest key: pk_dev_local_0123456789abcdef
INSERT INTO users (email, password_hash, name)
VALUES ('demo@pulse.local', '$2a$12$h.CvAs1mGxIDog4GXsVPqu3uuAOfQiDqtNG1TG1FoD8Enqm4tiIlC', 'Demo User')
ON CONFLICT DO NOTHING;

INSERT INTO project_members (project_id, user_id, role)
SELECT 1, id, 'owner' FROM users WHERE lower(email) = 'demo@pulse.local'
ON CONFLICT DO NOTHING;

UPDATE projects SET name = 'Demo Shop' WHERE id = 1 AND name = 'Default';

INSERT INTO api_keys (project_id, public_key, rate_limit_per_sec)
SELECT 1, 'pk_dev_local_0123456789abcdef', 1000
WHERE NOT EXISTS (SELECT 1 FROM api_keys WHERE project_id = 1);
