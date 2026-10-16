#!/usr/bin/env bash
# Measures end-to-end throughput on the local compose stack: k6 posts events
# and spans for DURATION at RATE requests/s (BATCH records each), then this
# waits until the stream processor has written every accepted record to
# ClickHouse. Prints ingest throughput (k6) and storage throughput (records
# written / time from start until the backlog drained).
#
#   load/run.sh                      # RATE=1000 BATCH=50 DURATION=60s
#   RATE=2000 load/run.sh
set -euo pipefail
cd "$(dirname "$0")"
RATE=${RATE:-1000} BATCH=${BATCH:-50} DURATION=${DURATION:-60s}
PROCESSOR_METRICS=${PROCESSOR_METRICS:-http://localhost:8082/metrics}
KEY=${PULSE_KEY:-pk_load_local_0123456789abcdef}

# Local stack only: a key whose rate limit does not cap the test.
docker exec -i postgres psql -U platform -d platform -q < setup-key.sql

written() {
  curl -fsS "$PROCESSOR_METRICS" | awk '/^pulse_processor_rows_written_total\{kind="(events|spans)"\}/ {s += $2} END {printf "%d\n", s}'
}

before=$(written)
start=$(date +%s.%N)
k6 run -q --summary-export=/tmp/pulse-k6-ingest.json -e PULSE_KEY="$KEY" -e RATE="$RATE" -e BATCH="$BATCH" -e DURATION="$DURATION" ingest.js >/dev/null || true
sent_end=$(date +%s.%N)
accepted=$(python3 -c "import json;print(int(json.load(open('/tmp/pulse-k6-ingest.json'))['metrics']['records_accepted']['count']))")
p95=$(python3 -c "import json;print(round(json.load(open('/tmp/pulse-k6-ingest.json'))['metrics']['http_req_duration']['p(95)'],1))")
failed=$(python3 -c "import json;print(json.load(open('/tmp/pulse-k6-ingest.json'))['metrics']['http_req_failed']['value'])")

# Wait (up to 10 minutes) until the processor has written them all.
deadline=$(( $(date +%s) + 600 ))
while [ $(( $(written) - before )) -lt "$accepted" ] && [ "$(date +%s)" -lt "$deadline" ]; do sleep 1; done
done_at=$(date +%s.%N)
stored=$(( $(written) - before ))

python3 - "$start" "$sent_end" "$done_at" "$accepted" "$stored" "$p95" "$failed" "$RATE" "$BATCH" <<'PY'
import sys
start, sent, done, accepted, stored, p95, failed, rate, batch = sys.argv[1:]
start, sent, done = float(start), float(sent), float(done)
accepted, stored = int(accepted), int(stored)
print(f"target          {int(rate)} req/s x {batch} records = {int(rate) * int(batch)} records/s")
print(f"ingest accepted {accepted} records in {sent - start:.0f}s = {accepted / (sent - start):,.0f} records/s, p95 {p95} ms, failed {float(failed) * 100:.2f}%")
print(f"stored          {stored} records in {done - start:.0f}s = {stored / (done - start):,.0f} records/s (backlog drained {done - sent:.0f}s after sending stopped)")
PY
