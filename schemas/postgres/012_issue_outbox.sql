-- Transactional outbox for issue events. Recording an occurrence that
-- creates or reopens an issue inserts its event here in the same
-- transaction; the stream processor relays unsent rows to Kafka
-- (issue.events) and marks them sent, so an event is never lost when Kafka
-- is unavailable. Consumers deduplicate redelivered events.
CREATE TABLE IF NOT EXISTS issue_outbox (
    id         BIGSERIAL PRIMARY KEY,
    issue_id   BIGINT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS issue_outbox_unsent ON issue_outbox (id) WHERE sent_at IS NULL;
CREATE INDEX IF NOT EXISTS issue_outbox_sent_at ON issue_outbox (sent_at) WHERE sent_at IS NOT NULL;
