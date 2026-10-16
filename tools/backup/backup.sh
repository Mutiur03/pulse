#!/usr/bin/env bash
# Back up Pulse: Postgres (pg_dump custom format), ClickHouse (each table in
# Native format with its CREATE statement) and object storage buckets.
#
#   tools/backup/backup.sh [output-dir]      # default backups/<UTC timestamp>
#
# Settings and MODE are described in common.sh; see docs/backup-restore.md.
set -euo pipefail
cd "$(dirname "$0")"
. ./common.sh

WORK=$(realpath -m "${1:-../../backups/$(date -u +%Y%m%dT%H%M%SZ)}")
mkdir -p "$WORK/clickhouse" "$WORK/objects"
[ "$MODE" = compose ] && chmod 777 "$WORK" "$WORK/objects"

log "postgres -> $WORK/postgres.dump"
if [ "$MODE" = compose ]; then
  pg pg_dump --format=custom --no-owner --file=/work/postgres.dump "$POSTGRES_URL"
else
  pg pg_dump --format=custom --no-owner --file="$WORK/postgres.dump" "$POSTGRES_URL"
fi

for t in $CLICKHOUSE_TABLES; do
  log "clickhouse $t"
  ch --query "SHOW CREATE TABLE $t" --format TSVRaw > "$WORK/clickhouse/$t.sql"
  # FINAL keeps one row per key for ReplacingMergeTree tables.
  final=""
  [ "$(ch --query "SELECT engine FROM system.tables WHERE database = currentDatabase() AND name = '$t'")" = ReplacingMergeTree ] && final=FINAL
  ch --query "SELECT * FROM $t $final FORMAT Native" | gzip > "$WORK/clickhouse/$t.native.gz"
  ch --query "SELECT count() FROM $t $final" > "$WORK/clickhouse/$t.count"
done

for b in $S3_BUCKETS; do
  log "objects $b"
  if [ "$MODE" = compose ]; then
    s3 sync --only-show-errors "s3://$b" "/work/objects/$b"
  else
    s3 sync --only-show-errors "s3://$b" "$WORK/objects/$b"
  fi
done

date -u +%Y-%m-%dT%H:%M:%SZ > "$WORK/created_at"
log "backup complete: $WORK ($(du -sh "$WORK" | cut -f1))"
echo "$WORK"
