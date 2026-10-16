// Shared helpers for the k6 load tests.
export const INGEST_URL = __ENV.INGEST_URL || 'http://localhost:8080';
export const API_URL = __ENV.API_URL || 'http://localhost:8081';
export const KEY = __ENV.PULSE_KEY || 'pk_dev_local_0123456789abcdef';
export const BATCH = Number(__ENV.BATCH || 50);
export const RATE = Number(__ENV.RATE || 200); // requests per second
export const DURATION = __ENV.DURATION || '60s';

const pages = ['/', '/pricing', '/docs', '/signup', '/checkout', '/blog/launch'];

export function hex(n) {
  let s = '';
  for (let i = 0; i < n; i++) s += Math.floor(Math.random() * 16).toString(16);
  return s;
}

export function events(n) {
  const out = [];
  const user = `load-${Math.floor(Math.random() * 100000)}`;
  const session = `ls-${hex(12)}`;
  const now = Date.now();
  for (let i = 0; i < n; i++) {
    const path = pages[i % pages.length];
    out.push({
      event: i % 10 === 9 ? 'purchase' : '$pageview',
      distinct_id: user,
      session_id: session,
      timestamp: new Date(now - (n - i) * 1000).toISOString(),
      properties: { $pathname: path, $current_url: `https://shop.example${path}`, amount: i % 10 === 9 ? 30 : undefined, plan: 'pro' },
    });
  }
  return out;
}

export function spans(n) {
  const out = [];
  const trace = hex(32);
  const root = hex(16);
  const now = Date.now();
  for (let i = 0; i < n; i++) {
    const db = i > 0 && i % 3 === 0;
    out.push({
      trace_id: trace,
      span_id: i === 0 ? root : hex(16),
      parent_span_id: i === 0 ? undefined : root,
      name: db ? 'SELECT orders' : i === 0 ? 'POST /api/pay' : 'payment-provider authorize',
      kind: i === 0 ? 'server' : 'client',
      service_name: 'load-api',
      timestamp: new Date(now - 200 + i).toISOString(),
      duration_ns: (5 + (i % 7) * 10) * 1e6,
      status_code: i % 50 === 0 && i > 0 ? 'error' : 'ok',
      http_route: i === 0 ? '/api/pay' : undefined,
      http_status: i === 0 ? 200 : undefined,
      db_system: db ? 'postgresql' : undefined,
      db_statement: db ? 'SELECT * FROM orders WHERE id = $1' : undefined,
    });
  }
  return out;
}
