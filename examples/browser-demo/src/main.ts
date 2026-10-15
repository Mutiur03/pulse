import { capture, flush, getClient, identify, init, reset } from '@pulse/browser';

const params = new URLSearchParams(location.search);
init({
  endpoint: params.get('ingest') ?? 'http://localhost:8080',
  projectId: '1',
  serviceName: 'browser-demo',
  environment: 'local',
  flushIntervalMs: 2000,
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
on('boom', () => {
  setTimeout(() => {
    throw new TypeError('demo: cannot read properties of undefined');
  });
  log('threw an error');
});
on('reject', () => {
  void Promise.reject(new Error('demo: unhandled rejection'));
  log('rejected a promise');
});

addEventListener('pagehide', () => void flush());
log(`session ${getClient()?.session.peek()}`);
