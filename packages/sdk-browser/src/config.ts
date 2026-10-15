import type { SpanExporter } from '@opentelemetry/sdk-trace-web';

export interface PulseConfig {
  /** Base URL of the ingest gateway, e.g. https://ingest.example.com */
  endpoint: string;
  /**
   * Public ingest key from project settings (pk_...). It identifies the
   * project; it is safe to ship in browser code and can be rotated.
   */
  apiKey: string;
  /** service.name for browser spans. Default "browser". */
  serviceName?: string;
  release?: string;
  environment?: string;
  /**
   * Cross-origin URLs that may receive traceparent/baggage headers.
   * Same-origin requests always get them. Anything else gets none.
   */
  propagateTraceHeaderCorsUrls?: (string | RegExp)[];
  /** Session inactivity timeout in ms. Default 30 minutes. */
  sessionTimeoutMs?: number;
  /** Autocapture switches. All default to true. */
  autocapture?: {
    clicks?: boolean;
    pageviews?: boolean;
    errors?: boolean;
    webVitals?: boolean;
    documentLoad?: boolean;
    userInteraction?: boolean;
  };
  /** Max events per request and flush interval. */
  batchSize?: number;
  flushIntervalMs?: number;
  /** Max queued items kept offline. Default 1000. */
  maxQueueSize?: number;
  /** Disable OTel tracing (events/errors still work). */
  tracing?: boolean;
  debug?: boolean;
  /**
   * Session replay. Off unless set. The recorder is a separate chunk that is
   * only downloaded for sampled sessions.
   */
  replay?: ReplayConfig;
  /** Replace the OTLP exporter, e.g. to send spans elsewhere or in tests. */
  spanExporter?: SpanExporter;
}

export type Properties = Record<string, unknown>;

export interface EventPayload {
  event: string;
  timestamp: string;
  distinct_id: string;
  session_id: string;
  trace_id?: string;
  span_id?: string;
  properties: Properties;
}

export interface ErrorPayload {
  timestamp: string;
  platform: 'browser';
  exception_type: string;
  message: string;
  stack: string;
  release?: string;
  environment?: string;
  trace_id?: string;
  span_id?: string;
  session_id: string;
  distinct_id: string;
  url: string;
}

export interface ReplayConfig {
  /** Share of sessions to record, 0 to 1. Default 1. */
  sampleRate?: number;
  /** Mask every input, textarea and select value. Default true. */
  maskAllInputs?: boolean;
  /** CSS selector whose text is replaced with asterisks. */
  maskTextSelector?: string;
  /** CSS selector of elements that are not recorded at all (shown as a blank box). */
  blockSelector?: string;
  /** Send a chunk at least this often. Default 5000 ms. */
  chunkIntervalMs?: number;
  /** Send a chunk early once its JSON reaches this size. Default 512 KB. */
  maxChunkBytes?: number;
}
