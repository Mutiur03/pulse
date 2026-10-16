/** Ingest endpoint as seen from the browser (the gateway on port 8080). */
function ingestURL() {
  return typeof window !== 'undefined' ? `${location.protocol}//${location.hostname}:8080` : 'http://localhost:8080';
}

export function browserSnippet(key = 'pk_…') {
  return `import { init, capture, identify } from '@pulse/browser';

init({
  endpoint: '${ingestURL()}',
  apiKey: '${key}',
  serviceName: 'web',
  // Upload source maps for this release to see original stack frames.
  release: 'web@1.0.0',
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

/** Query API base URL for pulse-cli (port 8081 locally). */
function apiURL() {
  return typeof window !== 'undefined' ? `${location.protocol}//${location.hostname}:8081` : 'http://localhost:8081';
}

export function uploadSnippet(project: number | string = 1, token = 'pul_…') {
  return `# After each build, with the same release as the SDK:
pulse-cli sourcemaps inject dist/
pulse-cli sourcemaps upload --release web@1.0.0 --url-prefix ~/assets \\
  --url ${apiURL()} --project ${project} --token ${token} dist/`;
}

/** Backend setup per language. Every SDK is a stock OpenTelemetry SDK
 * pointed at the ingest gateway with the project key as a header. */
export const backendPlatforms = ['node', 'python', 'go', 'other'] as const;
export type BackendPlatform = (typeof backendPlatforms)[number];

export const platformLabels: Record<'browser' | BackendPlatform, string> = {
  browser: 'Browser',
  node: 'Node.js',
  python: 'Python',
  go: 'Go',
  other: 'Any OpenTelemetry SDK',
};

export function installCommand(platform: 'browser' | BackendPlatform): string | undefined {
  switch (platform) {
    case 'browser':
      return 'npm install @pulse/browser';
    case 'node':
      return 'npm install @opentelemetry/sdk-node @opentelemetry/auto-instrumentations-node @opentelemetry/exporter-trace-otlp-proto';
    case 'python':
      return 'pip install opentelemetry-distro opentelemetry-exporter-otlp\nopentelemetry-bootstrap -a install';
    case 'go':
      return 'go get go.opentelemetry.io/otel go.opentelemetry.io/otel/sdk go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp';
  }
  return undefined;
}

export function platformSnippet(platform: 'browser' | BackendPlatform, key = 'pk_…'): string {
  const env = backendSnippet(key);
  switch (platform) {
    case 'browser':
      return browserSnippet(key);
    case 'node':
      return `// tracing.js: load first, e.g. node --require ./tracing.js server.js
const { NodeSDK } = require('@opentelemetry/sdk-node');
const { getNodeAutoInstrumentations } = require('@opentelemetry/auto-instrumentations-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-proto');

new NodeSDK({
  traceExporter: new OTLPTraceExporter(),
  instrumentations: [getNodeAutoInstrumentations()],
}).start();

# Environment (shell):
${env}`;
    case 'python':
      return `# Environment (shell):
${env}

# Run your app under the OpenTelemetry agent:
opentelemetry-instrument python app.py`;
    case 'go':
      return `// Uses OTEL_EXPORTER_OTLP_* from the environment.
exp, err := otlptracehttp.New(ctx)
if err != nil {
\tlog.Fatal(err)
}
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
defer tp.Shutdown(ctx)
otel.SetTracerProvider(tp)
otel.SetTextMapPropagator(propagation.TraceContext{})

http.ListenAndServe(":8000", otelhttp.NewHandler(mux, "server"))

# Environment (shell):
${env}`;
  }
  return `# Environment for any OpenTelemetry SDK or Collector:
${env}`;
}
