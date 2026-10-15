import {
  context as otelContext,
  propagation,
  trace,
  type Context,
  type TextMapPropagator,
  type TextMapSetter,
  type TextMapGetter,
} from '@opentelemetry/api';
import { CompositePropagator, W3CBaggagePropagator, W3CTraceContextPropagator } from '@opentelemetry/core';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { registerInstrumentations, type Instrumentation } from '@opentelemetry/instrumentation';
import { DocumentLoadInstrumentation } from '@opentelemetry/instrumentation-document-load';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { UserInteractionInstrumentation } from '@opentelemetry/instrumentation-user-interaction';
import { XMLHttpRequestInstrumentation } from '@opentelemetry/instrumentation-xml-http-request';
import { resourceFromAttributes } from '@opentelemetry/resources';
import {
  BatchSpanProcessor,
  WebTracerProvider,
  type ReadableSpan,
  type Span,
  type SpanProcessor,
} from '@opentelemetry/sdk-trace-web';
import type { PulseConfig } from './config';

export interface IdentitySource {
  sessionId(): string;
  distinctId(): string;
  userId(): string | undefined;
  onSpanStart?(span: Span): void;
}

export const ATTR_SESSION = 'session.id';
export const ATTR_USER = 'enduser.id';
export const ATTR_DISTINCT = 'pulse.distinct_id';

/** Stamps session and user ids on every span when it starts. */
export class IdentitySpanProcessor implements SpanProcessor {
  constructor(private ids: IdentitySource) {}
  onStart(span: Span): void {
    span.setAttribute(ATTR_SESSION, this.ids.sessionId());
    span.setAttribute(ATTR_DISTINCT, this.ids.distinctId());
    const user = this.ids.userId();
    if (user) span.setAttribute(ATTR_USER, user);
    this.ids.onSpanStart?.(span);
  }
  onEnd(_span: ReadableSpan): void {}
  forceFlush(): Promise<void> {
    return Promise.resolve();
  }
  shutdown(): Promise<void> {
    return Promise.resolve();
  }
}

/** Adds session.id and enduser.id as W3C baggage on outgoing requests. */
export class IdentityBaggagePropagator implements TextMapPropagator {
  private inner = new W3CBaggagePropagator();
  constructor(private ids: IdentitySource) {}

  inject(ctx: Context, carrier: unknown, setter: TextMapSetter): void {
    let bag = propagation.getBaggage(ctx) ?? propagation.createBaggage();
    bag = bag.setEntry(ATTR_SESSION, { value: this.ids.sessionId() });
    const user = this.ids.userId();
    if (user) bag = bag.setEntry(ATTR_USER, { value: user });
    this.inner.inject(propagation.setBaggage(ctx, bag), carrier, setter);
  }
  extract(ctx: Context, carrier: unknown, getter: TextMapGetter): Context {
    return this.inner.extract(ctx, carrier, getter);
  }
  fields(): string[] {
    return this.inner.fields();
  }
}

export function escapeRegExp(s: string): RegExp {
  return new RegExp('^' + s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
}

export interface Tracing {
  provider: WebTracerProvider;
  disable(): void;
}

export function startTracing(cfg: PulseConfig, ids: IdentitySource): Tracing {
  const endpoint = cfg.endpoint.replace(/\/$/, '');
  const exporter =
    cfg.spanExporter ??
    new OTLPTraceExporter({
      url: `${endpoint}/v1/traces?project_id=${encodeURIComponent(cfg.projectId)}`,
    });
  const resource = resourceFromAttributes({
    'service.name': cfg.serviceName ?? 'browser',
    'pulse.project_id': cfg.projectId,
    // Lets the backend tell browser spans apart (and skip deriving errors
    // from them, since browser errors are sent to /v1/errors directly).
    'telemetry.sdk.language': 'webjs',
    'telemetry.sdk.name': 'pulse-browser',
    ...(cfg.release ? { 'service.version': cfg.release } : {}),
    ...(cfg.environment ? { 'deployment.environment.name': cfg.environment } : {}),
  });
  const provider = new WebTracerProvider({
    resource,
    spanProcessors: [new IdentitySpanProcessor(ids), new BatchSpanProcessor(exporter)],
  });
  provider.register({
    propagator: new CompositePropagator({
      propagators: [new W3CTraceContextPropagator(), new IdentityBaggagePropagator(ids)],
    }),
  });

  // Never trace the SDK's own uploads.
  const ignoreUrls = [escapeRegExp(endpoint)];
  const cors = cfg.propagateTraceHeaderCorsUrls ?? [];
  const auto = cfg.autocapture ?? {};
  const instrumentations: Instrumentation[] = [
    new FetchInstrumentation({ propagateTraceHeaderCorsUrls: cors, ignoreUrls }),
    new XMLHttpRequestInstrumentation({ propagateTraceHeaderCorsUrls: cors, ignoreUrls }),
  ];
  if (auto.documentLoad !== false) instrumentations.push(new DocumentLoadInstrumentation());
  if (auto.userInteraction !== false) {
    instrumentations.push(new UserInteractionInstrumentation({ eventNames: ['click', 'submit'] }));
  }
  const disable = registerInstrumentations({ tracerProvider: provider, instrumentations });
  return { provider, disable };
}

/** Ids of the active span, if any. */
export function activeSpanIds(): { trace_id?: string; span_id?: string } {
  const span = trace.getSpan(otelContext.active());
  const sc = span?.spanContext();
  if (!sc || !trace.isSpanContextValid(sc)) return {};
  return { trace_id: sc.traceId, span_id: sc.spanId };
}
