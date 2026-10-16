#!/bin/bash
# Applies every Postgres and ClickHouse schema file in order. All files are
# idempotent, so running this on every upgrade is safe.
#
# Postgres: PGHOST, PGPORT, PGUSER, PGPASSWORD, PGDATABASE (or DATABASE_URL).
# ClickHouse: CLICKHOUSE_HOST, CLICKHOUSE_PORT (9000), CLICKHOUSE_USER,
# CLICKHOUSE_PASSWORD, CLICKHOUSE_SECURE=true for TLS.
set -euo pipefail

for f in $(ls /migrations/postgres/*.sql | sort); do
  echo "postgres: applying $f"
  if [ -n "${DATABASE_URL:-}" ]; then
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -f "$f"
  else
    psql -v ON_ERROR_STOP=1 -q -f "$f"
  fi
done

ch_args=(--host "${CLICKHOUSE_HOST:-clickhouse}" --port "${CLICKHOUSE_PORT:-9000}" --user "${CLICKHOUSE_USER:-default}")
if [ -n "${CLICKHOUSE_PASSWORD:-}" ]; then ch_args+=(--password "$CLICKHOUSE_PASSWORD"); fi
if [ "${CLICKHOUSE_SECURE:-false}" = "true" ]; then ch_args+=(--secure); fi
for f in $(ls /migrations/clickhouse/*.sql | sort); do
  echo "clickhouse: applying $f"
  clickhouse-client "${ch_args[@]}" --multiquery < "$f"
done
echo "migrations applied"
