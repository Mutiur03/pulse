-- SDK language of the emitting resource (webjs, go, python, ...), used to tell
-- browser spans from backend spans when building a trace tree.
ALTER TABLE spans ADD COLUMN IF NOT EXISTS platform LowCardinality(String) DEFAULT '' AFTER service_name;
