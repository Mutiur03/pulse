# 8. Postgres for metadata and control-plane state

Date: 2026-10-16. Status: accepted.

## Context

Users, organizations, projects, keys, issues, alert rules, dashboards,
releases, audit logs and deletion requests need transactions, constraints
and point lookups, which ClickHouse is not built for.

## Decision

Keep all control-plane state in Postgres 16, accessed with pgx. Schema files
in `schemas/postgres` are numbered and idempotent and are applied in order
at startup (Compose) or by the Helm migration Job. Redis holds only
disposable state: sessions, rate limits, key cache and alert locks.

## Consequences

- Strong consistency where it matters (issue status, key revocation, claims
  with `FOR UPDATE SKIP LOCKED` for deletion requests).
- Postgres is small but critical: it is backed up with `pg_dump`
  (tools/backup) and runs as a managed multi-AZ instance in production.
- Losing Redis logs users out and resets rate limits but loses no data.
