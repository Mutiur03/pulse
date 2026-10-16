#!/usr/bin/env bash
# Restore a backup made by backup.sh into an empty (freshly migrated) or
# existing Pulse installation.
#
#   tools/backup/restore.sh <backup-dir>
#
# Postgres objects are dropped and recreated (pg_restore --clean). ClickHouse
# tables are truncated, then reloaded. Objects are synced back to their
# buckets. Stop the Pulse services first so nothing writes during the restore.
set -euo pipefail
cd "$(dirname "$0")"
. ./common.sh

WORK=$(realpath "${1:?usage: restore.sh <backup-dir>}")
[ -f "$WORK/postgres.dump" ] || { echo "no postgres.dump in $WORK" >&2; exit 1; }

log "postgres <- $WORK/postgres.dump"
if [ "$MODE" = compose ]; then
  pg pg_restore --clean --if-exists --no-owner --dbname="$POSTGRES_URL" /work/postgres.dump
else
  pg pg_restore --clean --if-exists --no-owner --dbname="$POSTGRES_URL" "$WORK/postgres.dump"
fi

# trace_summaries is filled by a materialized view as spans are inserted, so
# empty it first rather than restoring it.
ch --query "TRUNCATE TABLE IF EXISTS trace_summaries"
for f in "$WORK"/clickhouse/*.native.gz; do
  t=$(basename "$f" .native.gz)
  log "clickhouse $t"
  # Create the table if the target has not been migrated yet.
  sed 's/^CREATE TABLE /CREATE TABLE IF NOT EXISTS /' "$WORK/clickhouse/$t.sql" | ch --multiquery
  ch --query "TRUNCATE TABLE $t"
  want=$(cat "$WORK/clickhouse/$t.count")
  [ "$want" = 0 ] || gunzip -c "$f" | ch --query "INSERT INTO $t FORMAT Native"
  got=$(ch --query "SELECT count() FROM $t")
  [ "$want" = "$got" ] || { echo "clickhouse $t: restored $got rows, expected $want" >&2; exit 1; }
done

for d in "$WORK"/objects/*/; do
  [ -d "$d" ] || continue
  b=$(basename "$d")
  log "objects $b"
  s3 mb "s3://$b" >/dev/null 2>&1 || true
  if [ "$MODE" = compose ]; then
    s3 sync --only-show-errors "/work/objects/$b" "s3://$b"
  else
    s3 sync --only-show-errors "$d" "s3://$b"
  fi
done
log "restore complete from $WORK"
