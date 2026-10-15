#!/usr/bin/env bash
# End-to-end smoke test: ingest gateway -> Kafka -> stream processor -> ClickHouse.
# Exits non-zero if the posted event or span does not show up in ClickHouse.
set -euo pipefail

INGEST_URL="${INGEST_URL:-http://localhost:8080}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://localhost:8123}"
TIMEOUT="${SMOKE_TIMEOUT:-90}"

RUN_ID="smoke_$(date +%s)_$RANDOM"
TRACE_ID="$(head -c16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
SPAN_ID="$(head -c8 /dev/urandom | od -An -tx1 | tr -d ' \n')"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ch() { curl -fsS "$CLICKHOUSE_URL/" --data-binary "$1"; }

fail() { echo "FAIL: $*" >&2; exit 1; }

wait_for() {
  local name="$1" url="$2" deadline=$((SECONDS + TIMEOUT))
  until curl -fsS "$url" >/dev/null 2>&1; do
    (( SECONDS < deadline )) || fail "$name not reachable at $url"
    sleep 2
  done
  echo "ok: $name is up"
}

wait_rows() {
  local label="$1" query="$2" deadline=$((SECONDS + TIMEOUT)) n
  while :; do
    n="$(ch "$query" 2>/dev/null || echo 0)"
    [[ "$n" =~ ^[0-9]+$ ]] || n=0
    if (( n > 0 )); then echo "ok: $label found in ClickHouse ($n row)"; return 0; fi
    (( SECONDS < deadline )) || fail "$label not found in ClickHouse after ${TIMEOUT}s"
    sleep 2
  done
}

echo "==> run id $RUN_ID, trace $TRACE_ID"
wait_for "ingest gateway" "$INGEST_URL/health"
wait_for "clickhouse" "$CLICKHOUSE_URL/ping"

echo "==> POST /v1/events"
code=$(curl -sS -o /dev/stderr -w '%{http_code}' -X POST "$INGEST_URL/v1/events" \
  -H 'Content-Type: application/json' \
  -d "{\"project_id\":\"1\",\"event\":\"\$pageview\",\"timestamp\":\"$NOW\",\"distinct_id\":\"$RUN_ID\",\"session_id\":\"sess_$RUN_ID\",\"trace_id\":\"$TRACE_ID\",\"properties\":{\"\$current_url\":\"https://example.com/home\"}}")
echo; [[ "$code" == 202 ]] || fail "events endpoint returned $code"

echo "==> POST /v1/traces"
code=$(curl -sS -o /dev/stderr -w '%{http_code}' -X POST "$INGEST_URL/v1/traces" \
  -H 'Content-Type: application/json' \
  -d "[{\"project_id\":\"1\",\"trace_id\":\"$TRACE_ID\",\"span_id\":\"$SPAN_ID\",\"name\":\"GET /api/checkout\",\"service_name\":\"smoke\",\"timestamp\":\"$NOW\",\"duration_ns\":1500000}]")
echo; [[ "$code" == 202 ]] || fail "traces endpoint returned $code"

echo "==> waiting for rows in ClickHouse"
wait_rows "event" "SELECT count() FROM events WHERE distinct_id = '$RUN_ID' AND trace_id = '$TRACE_ID'"
wait_rows "span"  "SELECT count() FROM spans WHERE trace_id = '$TRACE_ID' AND span_id = '$SPAN_ID'"

echo "==> joined view by trace_id"
ch "SELECT e.event, e.distinct_id, s.span_name, s.duration_ns FROM events e INNER JOIN spans s ON e.trace_id = s.trace_id WHERE e.trace_id = '$TRACE_ID' FORMAT PrettyCompactMonoBlock"

echo "PASS: event and span landed in ClickHouse"
