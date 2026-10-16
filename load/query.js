// Query API latency under concurrent dashboard use: trace list, event
// explorer, a trend insight and a funnel, by several logged-in users.
//
//   k6 run -e VUS=20 -e DURATION=60s load/query.js
//
// Trends and funnels are cached by ClickHouse for 60 s per minute-aligned
// range, so repeated runs of the same query are answered from the cache.
// VARY_RANGE=1 starts each request's range at a different minute, so every
// query is computed (the uncached worst case).
import http from 'k6/http';
import { check, sleep } from 'k6';
import { API_URL } from './lib.js';

const email = __ENV.PULSE_EMAIL || 'demo@pulse.local';
const password = __ENV.PULSE_PASSWORD || 'pulse-demo';

export const options = {
  scenarios: {
    dashboards: { executor: 'constant-vus', vus: Number(__ENV.VUS || 20), duration: __ENV.DURATION || '60s' },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:traces}': ['p(95)<1000'],
    'http_req_duration{name:events}': ['p(95)<1000'],
    'http_req_duration{name:trend}': ['p(95)<1500'],
    'http_req_duration{name:funnel}': ['p(95)<1500'],
  },
};

// One login shared by all virtual users (login is rate limited).
export function setup() {
  const res = http.post(`${API_URL}/v1/auth/login`, JSON.stringify({ email, password }), {
    headers: { 'Content-Type': 'application/json' },
  });
  check(res, { 'logged in': (r) => r.status === 200 });
  const cookie = res.cookies.pulse_session && res.cookies.pulse_session[0].value;
  const me = http.get(`${API_URL}/v1/auth/me`, { cookies: { pulse_session: cookie } }).json();
  return { cookie, project: me.projects[0].id };
}

export default function ({ cookie, project }) {
  const jar = http.cookieJar();
  jar.set(API_URL, 'pulse_session', cookie);
  const json = { 'Content-Type': 'application/json' };
  const shift = __ENV.VARY_RANGE ? Math.floor(Math.random() * 600) * 60 * 1000 : 0;
  const from = new Date(Date.now() - 24 * 3600 * 1000 - shift).toISOString();
  const to = new Date().toISOString();
  const get = (name, path) => check(http.get(`${API_URL}${path}`, { tags: { name } }), { [`${name} 200`]: (r) => r.status === 200 });

  get('traces', `/v1/traces?project_id=${project}&from=${encodeURIComponent(from)}&limit=100`);
  get('events', `/v1/events?project_id=${project}&from=${encodeURIComponent(from)}&limit=100`);
  const trend = http.post(
    `${API_URL}/v1/insights/trends?project_id=${project}`,
    JSON.stringify({ from, to, interval: 'hour', series: [{ event: '$pageview', math: 'unique_users' }] }),
    { headers: json, tags: { name: 'trend' } },
  );
  check(trend, { 'trend 200': (r) => r.status === 200 });
  const funnel = http.post(
    `${API_URL}/v1/funnels/query?project_id=${project}`,
    JSON.stringify({ from, to, steps: ['$pageview', 'purchase'] }),
    { headers: json, tags: { name: 'funnel' } },
  );
  check(funnel, { 'funnel 200': (r) => r.status === 200 });
  sleep(1);
}
