import { capture, flush, getClient, identify, init, reset } from '@pulse/browser';

const params = new URLSearchParams(location.search);
// The instrumented Go API from examples/go-demo-api (another origin).
const goApi = params.get('api') ?? 'http://localhost:8090';
init({
  endpoint: params.get('ingest') ?? 'http://localhost:8080',
  apiKey: params.get('key') ?? 'pk_dev_local_0123456789abcdef',
  serviceName: 'browser-demo',
  environment: 'local',
  // Source maps are uploaded for this release (see README).
  release: params.get('release') ?? 'browser-demo@dev',
  flushIntervalMs: 2000,
  // Session replay: record every session unless ?replay=0. Text under .private
  // is masked and .secret is not recorded at all.
  replay: { sampleRate: Number(params.get('replay') ?? 1), maskTextSelector: '.private', blockSelector: '.secret' },
  // Allow trace headers to the Go API so its spans join the browser trace.
  propagateTraceHeaderCorsUrls: [new RegExp('^' + goApi.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))],
});

const log = (msg: string) => {
  const el = document.getElementById('log')!;
  el.textContent = `${new Date().toLocaleTimeString()} ${msg}\n` + el.textContent;
};
const route = () => (document.getElementById('route')!.textContent = location.pathname);
route();

// Minimal SPA router so route changes show up as $pageview events.
document.querySelectorAll<HTMLAnchorElement>('a[data-route]').forEach((a) =>
  a.addEventListener('click', (e) => {
    e.preventDefault();
    history.pushState({}, '', a.getAttribute('href')!);
    route();
  }),
);
addEventListener('popstate', route);

const on = (id: string, fn: () => void) => document.getElementById(id)!.addEventListener('click', fn);

on('pay', async () => {
  const res = await fetch('/api/pay', { method: 'POST' });
  const body = await res.json();
  log(`/api/pay saw traceparent=${body.traceparent} baggage=${body.baggage}`);
  capture('payment_submitted', { amount: 42 });
});
on('orders', async () => {
  const res = await fetch(`${goApi}/api/orders?n=3`);
  const body = await res.json();
  log(`Go API orders: ${body.orders?.length} rows, trace ${body.trace_id}`);
});
on('gopay', async () => {
  const res = await fetch(`${goApi}/api/pay`, { method: 'POST' });
  const body = await res.json();
  log(`Go API pay: HTTP ${res.status} ${body.error ?? ''}, trace ${body.trace_id}`);
  (window as unknown as { lastTrace?: string }).lastTrace = body.trace_id;
});
// Shop flow used by tools/seed-demo: checkout_started -> payment_succeeded.
on('checkout', async () => {
  capture('checkout_started', { items: 3 });
  const res = await fetch(`${goApi}/api/orders?n=3`);
  log(`checkout: orders HTTP ${res.status}`);
});
on('buy', async () => {
  const fail = params.get('fail');
  const res = await fetch(`${goApi}/api/pay${fail ? `?fail=${fail}` : ''}`, { method: 'POST' });
  const body = await res.json();
  (window as unknown as { lastTrace?: string }).lastTrace = body.trace_id;
  if (res.ok) {
    capture('payment_succeeded', { amount: 42 });
    log(`paid, trace ${body.trace_id}`);
  } else {
    capture('payment_failed', { status: res.status });
    log(`payment failed: HTTP ${res.status} ${body.error ?? ''}`);
    setTimeout(() => {
      throw new Error(`Payment failed: ${body.error ?? res.status}`);
    });
  }
});
on('xhr', () => {
  const xhr = new XMLHttpRequest();
  xhr.open('GET', '/api/cart');
  xhr.onload = () => log(`/api/cart: ${xhr.responseText}`);
  xhr.send();
});
on('login', () => {
  identify('user_42', { plan: 'pro' });
  log('identified as user_42');
});
on('logout', () => {
  reset();
  log(`reset, new anonymous id ${getClient()?.identity.distinctId}`);
});
function readCart(cart?: { items: string[] }): string[] {
  if (!cart) {
    throw new TypeError('demo: cannot read properties of undefined');
  }
  return cart.items;
}
on('boom', () => {
  setTimeout(() => readCart());
  log('threw an error');
});
on('reject', () => {
  void Promise.reject(new Error('demo: unhandled rejection'));
  log('rejected a promise');
});

addEventListener('pagehide', () => void flush());
const session = getClient()?.session.peek();
(window as unknown as { pulseSession?: string }).pulseSession = session;
log(`session ${session}`);
