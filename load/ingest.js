// Ingest throughput: product events and spans posted to the ingest gateway
// at a constant request rate. Each request carries BATCH records.
//
//   k6 run -e RATE=200 -e BATCH=50 -e DURATION=60s load/ingest.js
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { BATCH, DURATION, INGEST_URL, KEY, RATE, events, spans } from './lib.js';

const records = new Counter('records_accepted');

export const options = {
  discardResponseBodies: false,
  scenarios: {
    events: {
      executor: 'constant-arrival-rate',
      rate: Math.ceil(RATE / 2),
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 50,
      maxVUs: 400,
      exec: 'sendEvents',
    },
    spans: {
      executor: 'constant-arrival-rate',
      rate: Math.floor(RATE / 2),
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 50,
      maxVUs: 400,
      exec: 'sendSpans',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{scenario:events}': ['p(95)<500'],
    'http_req_duration{scenario:spans}': ['p(95)<500'],
  },
};

const params = { headers: { 'Content-Type': 'application/json', 'X-Pulse-Key': KEY } };

function send(path, body) {
  const res = http.post(`${INGEST_URL}${path}`, JSON.stringify(body), params);
  if (check(res, { accepted: (r) => r.status === 202 })) records.add(body.length);
}

export function sendEvents() {
  send('/v1/events', events(BATCH));
}

export function sendSpans() {
  send('/v1/traces', spans(BATCH));
}
