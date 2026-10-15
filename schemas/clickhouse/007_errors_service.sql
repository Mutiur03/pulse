-- Backend errors carry the emitting service and the top in-app frame.
ALTER TABLE errors ADD COLUMN IF NOT EXISTS service_name LowCardinality(String) DEFAULT '' AFTER platform;
ALTER TABLE errors ADD COLUMN IF NOT EXISTS culprit String DEFAULT '' AFTER stack;
ALTER TABLE errors ADD COLUMN IF NOT EXISTS title String DEFAULT '' AFTER exception_type;
