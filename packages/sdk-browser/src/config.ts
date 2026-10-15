export interface PulseConfig {
  /** Base URL of the ingest gateway, e.g. https://ingest.example.com */
  endpoint: string;
  /** Project the data belongs to. */
  projectId: string;
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
}

export type Properties = Record<string, unknown>;

export interface EventPayload {
  project_id: string;
  event: string;
  timestamp: string;
  distinct_id: string;
  session_id: string;
  trace_id?: string;
  span_id?: string;
  properties: Properties;
}

export interface ErrorPayload {
  project_id: string;
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
