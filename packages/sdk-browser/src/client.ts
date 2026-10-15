import { SpanStatusCode, trace } from '@opentelemetry/api';
import type { ErrorPayload, EventPayload, Properties, PulseConfig } from './config';
import { Identity } from './identity';
import { describeElement, safeUrl } from './privacy';
import { DEFAULT_SESSION_TIMEOUT, Session } from './session';
import { activeSpanIds, startTracing, type Tracing } from './tracing';
import { Transport } from './transport';

const TRACER = '@pulse/browser';

export class PulseClient {
  readonly identity = new Identity();
  readonly session: Session;
  readonly events: Transport<EventPayload>;
  readonly errors: Transport<ErrorPayload>;
  private tracing?: Tracing;
  private cleanups: (() => void)[] = [];
  private lastPath?: string;

  constructor(readonly config: PulseConfig) {
    const endpoint = config.endpoint.replace(/\/$/, '');
    const q = `?project_id=${encodeURIComponent(config.projectId)}`;
    this.session = new Session(config.sessionTimeoutMs ?? DEFAULT_SESSION_TIMEOUT);
    const t = {
      batchSize: config.batchSize,
      flushIntervalMs: config.flushIntervalMs,
      maxQueueSize: config.maxQueueSize,
    };
    this.events = new Transport({ url: `${endpoint}/v1/events${q}`, storageKey: 'pulse_q_events', ...t });
    this.errors = new Transport({ url: `${endpoint}/v1/errors${q}`, storageKey: 'pulse_q_errors', ...t });
  }

  start(): void {
    const auto = this.config.autocapture ?? {};
    if (this.config.tracing !== false) {
      this.tracing = startTracing(this.config, {
        sessionId: () => this.session.peek(),
        distinctId: () => this.identity.distinctId,
        userId: () => this.identity.userId,
      });
    }
    this.session.setRotateListener((id) => this.capture('$session_start', { $session_id: id }));
    this.listen(window, 'pagehide', () => this.beacon());
    this.listen(document, 'visibilitychange', () => {
      if (document.visibilityState === 'hidden') this.beacon();
    });
    this.listen(window, 'online', () => void this.flush());

    if (auto.pageviews !== false) this.watchPageviews();
    if (auto.clicks !== false) {
      this.listen(document, 'click', (e) => {
        const target = e.target;
        if (target instanceof Element) this.capture('$autocapture', { $event_type: 'click', ...describeElement(target) });
      }, true);
    }
    if (auto.errors !== false) this.watchErrors();
    if (auto.webVitals !== false) void this.watchVitals();
  }

  capture(event: string, properties: Properties = {}): void {
    const payload: EventPayload = {
      project_id: this.config.projectId,
      event,
      timestamp: new Date().toISOString(),
      distinct_id: this.identity.distinctId,
      session_id: this.session.touch(),
      ...activeSpanIds(),
      properties: {
        $current_url: safeUrl(location.href),
        $pathname: location.pathname,
        $lib: 'pulse-browser',
        ...properties,
      },
    };
    this.events.push(payload);
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
    this.errors.push({
      project_id: this.config.projectId,
      timestamp: new Date().toISOString(),
      platform: 'browser',
      exception_type: e.name || 'Error',
      message: e.message,
      stack: e.stack ?? '',
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
    await Promise.all([this.events.flush(), this.errors.flush(), this.tracing?.provider.forceFlush()]);
  }

  async shutdown(): Promise<void> {
    this.cleanups.forEach((fn) => fn());
    this.cleanups = [];
    await this.flush();
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
      this.capture('$pageview', { $title: document.title, $referrer: document.referrer ? safeUrl(document.referrer) : '' });
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
      });
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
