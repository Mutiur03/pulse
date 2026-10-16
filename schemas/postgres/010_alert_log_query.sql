-- Log count alerts store their search query.
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS log_query TEXT NOT NULL DEFAULT '';
