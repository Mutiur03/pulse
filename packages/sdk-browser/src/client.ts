import { SpanKind, SpanStatusCode, trace } from '@opentelemetry/api';
import type { ErrorPayload, EventPayload, Properties, PulseConfig } from './config';
import { Identity } from './identity';
import { describeElement, safeUrl } from './privacy';
import { DEFAULT_SESSION_TIMEOUT, Session } from './session';
import { activeSpanIds, startTracing, type Tracing } from './tracing';
import { Transport } from './transport';
import { sampled, type ReplayHandle } from './replay';
import { debugIdsFor } from './debugids';

const TRACER = '@pulse/browser';

export class PulseClient {
  readonly identity = new Identity();
  readonly session: Session;
  readonly events: Transport<EventPayload>;
  readonly errors: Transport<ErrorPayload>;
  private tracing?: Tracing;
  private cleanups: (() => void)[] = [];
  private lastPath?: string;
  private pendingClick?: EventPayload;
  /** Trace of the current page view; web vitals are reported on it. */
  private pageIds: { trace_id?: string; span_id?: string } = {};
  /** Set once the lazily loaded recorder is running. */
  replay?: ReplayHandle;
  private replayUrl: string;
  private stopped = false;

  constructor(readonly config: PulseConfig) {
    const endpoint = config.endpoint.replace(/\/$/, '');
    // sendBeacon cannot set headers, so the key travels as a query parameter.
    const q = `?key=${encodeURIComponent(config.apiKey)}`;
    this.session = new Session(config.sessionTimeoutMs ?? DEFAULT_SESSION_TIMEOUT);
    const t = {
      batchSize: config.batchSize,
      flushIntervalMs: config.flushIntervalMs,
      maxQueueSize: config.maxQueueSize,
    };
    this.events = new Transport({ url: `${endpoint}/v1/events${q}`, storageKey: 'pulse_q_events', ...t });
    this.errors = new Transport({ url: `${endpoint}/v1/errors${q}`, storageKey: 'pulse_q_errors', ...t });
    this.replayUrl = `${endpoint}/v1/replay${q}`;
  }

  /** Starts session replay if configured and this session is sampled. */
  private async startReplay(): Promise<void> {
    const rc = this.config.replay;
    if (!rc || !sampled(this.session.peek(), rc.sampleRate ?? 1)) return;
    const { startRecorder } = await import('./recorder');
    if (this.stopped) return;
    this.replay = startRecorder({
      url: this.replayUrl,
      sessionId: () => this.session.peek(),
      distinctId: () => this.identity.distinctId,
      config: rc,
    });
    if (this.pageIds.trace_id) this.replay.mark('pageview', { trace_id: this.pageIds.trace_id, path: location.pathname });
  }

  start(): void {
    const auto = this.config.autocapture ?? {};
    // Registered before tracing starts so the user-interaction
    // instrumentation does not wrap it: otherwise every click would get an
    // extra span for the SDK's own listener on top of the app's handler.
    if (auto.clicks !== false) {
      this.listen(document, 'click', (e) => {
        const target = e.target;
        if (!(target instanceof Element)) return;
        const payload = this.capture('$autocapture', { $event_type: 'click', ...describeElement(target) });
        // The app's click handler span starts right after this listener; the
        // span processor below copies its ids onto this event.
        this.pendingClick = payload;
        setTimeout(() => {
          if (this.pendingClick === payload) this.pendingClick = undefined;
        });
      }, true);
    }
    if (this.config.tracing !== false) {
      this.tracing = startTracing(this.config, {
        sessionId: () => this.session.peek(),
        distinctId: () => this.identity.distinctId,
        userId: () => this.identity.userId,
        onSpanStart: (span) => {
          if (span.kind === SpanKind.CLIENT) {
            this.replay?.mark('request', { trace_id: span.spanContext().traceId, name: span.name });
          }
          const p = this.pendingClick;
          if (p && !p.trace_id && span.name === 'click') {
            const sc = span.spanContext();
            p.trace_id = sc.traceId;
            p.span_id = sc.spanId;
            this.pendingClick = undefined;
          }
        },
      });
    }
    this.session.setRotateListener((id) => this.capture('$session_start', { $session_id: id }));
    this.listen(window, 'pagehide', () => this.beacon());
    this.listen(document, 'visibilitychange', () => {
      if (document.visibilityState === 'hidden') this.beacon();
    });
    this.listen(window, 'online', () => void this.flush());

    if (auto.pageviews !== false) this.watchPageviews();
    if (auto.errors !== false) this.watchErrors();
    if (auto.webVitals !== false) void this.watchVitals();
    void this.startReplay().catch(() => undefined);
  }

  capture(event: string, properties: Properties = {}, ids: { trace_id?: string; span_id?: string } = activeSpanIds()): EventPayload {
    const payload: EventPayload = {
      event,
      timestamp: new Date().toISOString(),
      distinct_id: this.identity.distinctId,
      session_id: this.session.touch(),
      ...ids,
      properties: {
        $current_url: safeUrl(location.href),
        $pathname: location.pathname,
        $lib: 'pulse-browser',
        ...properties,
      },
    };
    this.events.push(payload);
    return payload;
  }

  identify(userId: string, traits: Properties = {}): void {
    const previous = this.identity.identify(userId);
    if (previous !== undefined) this.capture('$identify', { $anon_distinct_id: previous, ...traits });
  }

  reset(): void {
    this.identity.reset();
    this.session.reset();
  }

  captureException(err: unknown, extra: { handled?: boolean } = {}): void {
    const e = err instanceof Error ? err : new Error(typeof err === 'string' ? err : safeStringify(err));
    let ids = activeSpanIds();
    if (!ids.trace_id && this.tracing) {
      // No request in flight: record the error on its own span so it is still traceable.
      const span = trace.getTracer(TRACER).startSpan('exception');
      span.recordException(e);
      span.setStatus({ code: SpanStatusCode.ERROR, message: e.message });
      const sc = span.spanContext();
      ids = { trace_id: sc.traceId, span_id: sc.spanId };
      span.end();
    }
    this.replay?.mark('error', { trace_id: ids.trace_id, type: e.name || 'Error', message: e.message.slice(0, 200) });
    this.errors.push({
      timestamp: new Date().toISOString(),
      platform: 'browser',
      exception_type: e.name || 'Error',
      message: e.message,
      stack: e.stack ?? '',
      debug_ids: debugIdsFor(e.stack ?? ''),
      release: this.config.release,
      environment: this.config.environment,
      ...ids,
      session_id: this.session.touch(),
      distinct_id: this.identity.distinctId,
      url: safeUrl(location.href),
    });
    if (extra.handled === false) void this.errors.flush();
  }

  async flush(): Promise<void> {
    await Promise.all([this.events.flush(), this.errors.flush(), this.tracing?.provider.forceFlush(), this.replay?.flush()]);
  }

  async shutdown(): Promise<void> {
    this.stopped = true;
    this.cleanups.forEach((fn) => fn());
    this.cleanups = [];
    await this.flush();
    this.replay?.stop();
    this.events.stop();
    this.errors.stop();
    if (this.tracing) {
      this.tracing.disable();
      await this.tracing.provider.shutdown();
    }
  }

  private beacon(): void {
    this.events.beacon();
    this.errors.beacon();
    this.replay?.beacon();
    void this.tracing?.provider.forceFlush();
  }

  private listen(t: EventTarget, type: string, fn: (e: Event) => void, capture = false): void {
    t.addEventListener(type, fn, capture);
    this.cleanups.push(() => t.removeEventListener(type, fn, capture));
  }

  private watchPageviews(): void {
    const fire = () => {
      const path = location.pathname + location.search;
      if (path === this.lastPath) return;
      this.lastPath = path;
      // Inside a navigation click the click's trace is active; otherwise give
      // the page view its own short span so it is always traceable.
      let ids = activeSpanIds();
      if (!ids.trace_id && this.tracing) {
        const span = trace.getTracer(TRACER).startSpan('pageview', { attributes: { 'url.path': location.pathname } });
        const sc = span.spanContext();
        ids = { trace_id: sc.traceId, span_id: sc.spanId };
        span.end();
      }
      this.pageIds = ids;
      this.replay?.mark('pageview', { trace_id: ids.trace_id, path: location.pathname });
      this.capture('$pageview', { $title: document.title, $referrer: document.referrer ? safeUrl(document.referrer) : '' }, ids);
    };
    fire();
    // SPA route changes: wrap pushState/replaceState and listen for back/forward.
    const h = history;
    const wrap = (key: 'pushState' | 'replaceState') => {
      const orig = h[key];
      h[key] = function (this: History, ...args: Parameters<History['pushState']>) {
        const r = orig.apply(this, args);
        queueMicrotask(fire);
        return r;
      };
      this.cleanups.push(() => {
        h[key] = orig;
      });
    };
    wrap('pushState');
    wrap('replaceState');
    this.listen(window, 'popstate', fire);
    this.listen(window, 'hashchange', fire);
  }

  private watchErrors(): void {
    this.listen(window, 'error', (e) => {
      const ev = e as ErrorEvent;
      // Resource load errors (img/script) have no ErrorEvent.error and no message.
      if (!ev.message && !ev.error) return;
      this.captureException(ev.error ?? new Error(ev.message), { handled: false });
    });
    this.listen(window, 'unhandledrejection', (e) => {
      this.captureException((e as PromiseRejectionEvent).reason, { handled: false });
    });
  }

  private async watchVitals(): Promise<void> {
    // Loaded lazily so bundlers can split it out.
    const { onLCP, onINP, onCLS, onTTFB, onFCP } = await import('web-vitals');
    const report = (m: { name: string; value: number; rating: string; id: string; navigationType?: string }) =>
      this.capture('$web_vitals', {
        $metric: m.name,
        $value: m.value,
        $rating: m.rating,
        $metric_id: m.id,
        $navigation_type: m.navigationType,
      }, this.pageIds);
    onLCP(report);
    onINP(report);
    onCLS(report);
    onTTFB(report);
    onFCP(report);
  }
}

function safeStringify(v: unknown): string {
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}
