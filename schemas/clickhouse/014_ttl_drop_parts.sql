-- Tables partitioned by day expire whole partitions at once instead of
-- rewriting parts row by row when their TTL passes. Per-project retention
-- shorter than these TTLs is enforced by the governance worker.
ALTER TABLE spans MODIFY SETTING ttl_only_drop_parts = 1;
ALTER TABLE logs MODIFY SETTING ttl_only_drop_parts = 1;
ALTER TABLE metrics MODIFY SETTING ttl_only_drop_parts = 1;
