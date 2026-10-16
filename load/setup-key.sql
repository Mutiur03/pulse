-- Local load testing only: an ingest key for the demo project with a high
-- request rate limit (keys default to 500 requests/s).
INSERT INTO api_keys (project_id, public_key, rate_limit_per_sec)
SELECT id, 'pk_load_local_0123456789abcdef', 100000 FROM projects ORDER BY id LIMIT 1
ON CONFLICT (public_key) DO UPDATE SET rate_limit_per_sec = EXCLUDED.rate_limit_per_sec, revoked_at = NULL;
