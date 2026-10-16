# Shared settings for backup.sh and restore.sh.
#
# MODE=compose (default) runs every client inside the local compose stack's
# containers. MODE=direct uses pg_dump/psql, clickhouse-client and the aws
# CLI from PATH against the endpoints below.

MODE=${MODE:-compose}
NETWORK=${COMPOSE_NETWORK:-compose_default}

# Postgres: a libpq URL.
POSTGRES_URL=${POSTGRES_URL:-postgres://platform:platform@postgres:5432/platform?sslmode=disable}
# ClickHouse native protocol.
CLICKHOUSE_HOST=${CLICKHOUSE_HOST:-clickhouse}
CLICKHOUSE_PORT=${CLICKHOUSE_PORT:-9000}
CLICKHOUSE_USER=${CLICKHOUSE_USER:-default}
CLICKHOUSE_PASSWORD=${CLICKHOUSE_PASSWORD:-}
CLICKHOUSE_DB=${CLICKHOUSE_DB:-default}
CLICKHOUSE_TABLES=${CLICKHOUSE_TABLES:-events sessions person_aliases spans errors logs metrics replay_chunks}
# S3-compatible storage.
S3_URL=${S3_URL:-http://objectstore:7070}
S3_ACCESS_KEY=${S3_ACCESS_KEY:-pulse}
S3_SECRET_KEY=${S3_SECRET_KEY:-pulse-local-secret}
S3_BUCKETS=${S3_BUCKETS:-pulse-replays pulse-artifacts}

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

# psql/pg_dump/pg_restore with the given args; stdin and stdout pass through.
pg() {
  local tool=$1; shift
  if [ "$MODE" = compose ]; then
    docker run --rm -i --network "$NETWORK" -v "$WORK:/work" postgres:16-alpine "$tool" "$@"
  else
    "$tool" "$@"
  fi
}

ch() {
  local args=(--host "$CLICKHOUSE_HOST" --port "$CLICKHOUSE_PORT" --user "$CLICKHOUSE_USER" --database "$CLICKHOUSE_DB")
  [ -n "$CLICKHOUSE_PASSWORD" ] && args+=(--password "$CLICKHOUSE_PASSWORD")
  [ "${CLICKHOUSE_SECURE:-false}" = true ] && args+=(--secure)
  if [ "$MODE" = compose ]; then
    docker run --rm -i --network "$NETWORK" clickhouse/clickhouse-server:24.10 clickhouse-client "${args[@]}" "$@"
  else
    clickhouse-client "${args[@]}" "$@"
  fi
}

s3() {
  if [ "$MODE" = compose ]; then
    docker run --rm -i --network "$NETWORK" -v "$WORK:/work" \
      -e AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY" -e AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY" -e AWS_DEFAULT_REGION=us-east-1 \
      amazon/aws-cli:2.27.50 --endpoint-url "$S3_URL" s3 "$@"
  else
    AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY" AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY" aws --endpoint-url "$S3_URL" s3 "$@"
  fi
}
