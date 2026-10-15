# @pulse/browser

Browser SDK for Pulse. One script gives you product analytics events, error
tracking, web vitals and OpenTelemetry traces that continue into your backend.

```ts
import { init, capture, identify, reset, shutdown } from '@pulse/browser';

init({
  endpoint: 'https://ingest.example.com',
  projectId: '1',
  serviceName: 'web',
  // traceparent/baggage are only sent same-origin and to these URLs
  propagateTraceHeaderCorsUrls: [/^https:\/\/api\.example\.com/],
});

capture('checkout_started', { plan: 'pro' });
identify('user_42');   // after login
reset();               // on logout
await shutdown();      // flush and stop
```

## What it records

| Data | How | Where it goes |
| --- | --- | --- |
| Traces | OTel `WebTracerProvider`; fetch, XHR, document-load and user-interaction instrumentation | `POST /v1/traces` (OTLP/HTTP JSON) |
| Pageviews | initial load plus SPA route changes (`pushState`, `replaceState`, `popstate`, `hashchange`) | `$pageview` events |
| Clicks | tag, selector, id, visible text (max 64 chars), link path | `$autocapture` events |
| Errors | `window.onerror`, `unhandledrejection`, `captureException()`; stack plus the active `trace_id`/`span_id` | `POST /v1/errors` |
| Web vitals | LCP, INP, CLS, TTFB, FCP via `web-vitals` (lazy-loaded chunk) | `$web_vitals` events |

Every span carries `session.id`, `pulse.distinct_id` and, once identified,
`enduser.id`. The same ids go out as W3C `baggage` next to `traceparent`, so
backend spans can be joined to the user and session.

## Privacy

- Values of inputs, textareas, selects and contenteditable elements are never read. For form fields only `type` and `name` are recorded.
- Add `data-pulse-no-capture` (or class `pulse-no-capture`) to skip text inside any element.
- Query strings and hashes are stripped from recorded URLs.
- Trace headers never go to origins outside `propagateTraceHeaderCorsUrls`.

## Delivery

Events and errors are batched (50 items or 5 s), mirrored to `localStorage`
so they survive reloads and offline periods (capped at 1000 items), retried
with exponential backoff up to 60 s, and sent with `sendBeacon` when the page
is hidden. Requests use `text/plain` so they don't need a CORS preflight.

## Sessions and identity

A session ends after 30 minutes without activity (`sessionTimeoutMs`) and is
shared across tabs. The anonymous id is stored in `localStorage`;
`identify()` switches `distinct_id` to the user id and emits `$identify` with
the previous anonymous id so the two can be merged.

## Build

`pnpm build` produces tree-shakeable ESM (`dist/index.js`) and CJS
(`dist/index.cjs`) with types. `pnpm size` prints the minified and gzipped
size, and `pnpm test` runs the vitest + jsdom suite.
