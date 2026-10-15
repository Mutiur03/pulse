#!/bin/bash
set -e

echo "==> Smoke Test: Ingest → Kafka → ClickHouse"
echo ""

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Step 1: Sending test event to ingest gateway${NC}"
curl -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "1",
    "event": "$pageview",
    "timestamp": "2026-10-15T05:00:00Z",
    "distinct_id": "user_123",
    "session_id": "sess_abc",
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "properties": {
      "$current_url": "https://example.com/home",
      "title": "Home Page"
    }
  }'
echo ""
echo ""

echo -e "${BLUE}Step 2: Sending test trace to ingest gateway${NC}"
curl -X POST http://localhost:8080/v1/traces \
  -H "Content-Type: application/json" \
  -d '[{
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "span_id": "00f067aa0ba902b7",
    "name": "GET /api/checkout"
  }]'
echo ""
echo ""

echo -e "${BLUE}Step 3: Waiting 10 seconds for stream processor to consume and write${NC}"
sleep 10

echo -e "${BLUE}Step 4: Querying ClickHouse for events${NC}"
docker exec clickhouse clickhouse-client --query "
SELECT 
  project_id, 
  event, 
  timestamp, 
  distinct_id, 
  session_id, 
  trace_id,
  properties
FROM events 
WHERE project_id = 1 
ORDER BY timestamp DESC 
LIMIT 5
FORMAT Vertical"
echo ""

echo -e "${BLUE}Step 5: Querying ClickHouse for spans${NC}"
docker exec clickhouse clickhouse-client --query "
SELECT 
  project_id, 
  trace_id, 
  span_id, 
  span_name, 
  timestamp
FROM spans 
WHERE project_id = 1 
ORDER BY timestamp DESC 
LIMIT 5
FORMAT Vertical"
echo ""

echo -e "${GREEN}✓ Smoke test complete!${NC}"
echo ""
echo "If you see data above, the end-to-end flow is working:"
echo "  1. Ingest gateway received the requests"
echo "  2. Data was produced to Kafka"
echo "  3. Stream processor consumed from Kafka"
echo "  4. Data was inserted into ClickHouse"
