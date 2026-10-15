/** Ingest endpoint as seen from the browser (the gateway on port 8080). */
export function ingestURL() {
  return typeof window !== 'undefined' ? `${location.protocol}//${location.hostname}:8080` : 'http://localhost:8080';
}

export function browserSnippet(key = 'pk_…') {
  return `import { init, capture, identify } from '@pulse/browser';

init({
  endpoint: '${ingestURL()}',
  apiKey: '${key}',
  serviceName: 'web',
  // Send traceparent/baggage to your API so its spans join browser traces.
  propagateTraceHeaderCorsUrls: [/^https:\\/\\/api\\.example\\.com/],
});

identify('user_123', { plan: 'pro' });
capture('checkout_started', { items: 3 });`;
}

export function backendSnippet(key = 'pk_…') {
  return `OTEL_EXPORTER_OTLP_ENDPOINT=${ingestURL()}
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS=X-Pulse-Key=${key}
OTEL_SERVICE_NAME=checkout-api`;
}
