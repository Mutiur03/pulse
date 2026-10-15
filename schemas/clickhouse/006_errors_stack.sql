-- Store the raw stack trace and the anonymous/identified user id with each error.
ALTER TABLE errors ADD COLUMN IF NOT EXISTS stack String DEFAULT '' AFTER message;
ALTER TABLE errors ADD COLUMN IF NOT EXISTS distinct_id String DEFAULT '' AFTER session_id;
