#!/usr/bin/env bash
# OTLP/gRPC smoke test with the OpenTelemetry telemetrygen tool: sends traces,
# metrics and logs to the gateway's gRPC receiver (:4317) and waits for them
# in ClickHouse. Also checks that a wrong key is refused. Needs Docker.
set -euo pipefail

ENDPOINT="${OTLP_GRPC_ENDPOINT:-localhost:4317}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://localhost:8123}"
TIMEOUT="${SMOKE_TIMEOUT:-90}"
KEY="${PULSE_KEY:-pk_dev_local_0123456789abcdef}"
IMAGE="${TELEMETRYGEN_IMAGE:-ghcr.io/open-telemetry/opentelemetry-collector-contrib/telemetrygen:v0.111.0}"
SERVICE="grpc-smoke-$(date +%s)-$RANDOM"

fail() { echo "FAIL: $*" >&2; exit 1; }

gen() {
  docker run --rm --network host "$IMAGE" "$@" --otlp-endpoint "$ENDPOINT" --otlp-insecure \
    --otlp-header "x-pulse-key=\"$KEY\""
}

wait_rows() {
  local label="$1" query="$2" deadline=$((SECONDS + TIMEOUT)) n
  while :; do
    n="$(curl -fsS "$CLICKHOUSE_URL/" --data-binary "$query" 2>/dev/null || echo 0)"
    [[ "$n" =~ ^[0-9]+$ ]] || n=0
    if (( n > 0 )); then echo "ok: $label in ClickHouse ($n rows)"; return 0; fi
    (( SECONDS < deadline )) || fail "$label not found in ClickHouse after ${TIMEOUT}s"
    sleep 2
  done
}

gen traces --traces 5 --service "$SERVICE"
gen metrics --metrics 5 --metric-type Sum --otlp-attributes "service.name=\"$SERVICE\""
gen logs --logs 5 --otlp-attributes "service.name=\"$SERVICE\""

wait_rows "spans" "SELECT count() FROM spans WHERE service_name = '$SERVICE'"
wait_rows "metric points" "SELECT count() FROM metrics WHERE service_name = '$SERVICE'"
wait_rows "logs" "SELECT count() FROM logs WHERE service_name = '$SERVICE'"

# A wrong key must be refused (telemetrygen logs the export error).
out="$(docker run --rm --network host "$IMAGE" traces --traces 1 --otlp-endpoint "$ENDPOINT" --otlp-insecure \
  --otlp-header 'x-pulse-key="pk_wrong"' 2>&1 || true)"
grep -q "Unauthenticated" <<<"$out" || fail "wrong key was not refused: $out"
echo "ok: wrong key refused with Unauthenticated"
echo "PASS: OTLP/gRPC smoke"
