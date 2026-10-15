import { InMemorySpanExporter } from '@opentelemetry/sdk-trace-base';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { PulseClient } from '../src/client';
import type { EventPayload } from '../src/config';

describe('click and pageview tracing', () => {
  const exporter = new InMemorySpanExporter();
  let client: PulseClient;
  let pushed: EventPayload[];

  beforeAll(() => {
    localStorage.clear();
    globalThis.fetch = vi.fn(async () => new Response('{}', { status: 200 })) as typeof fetch;
    document.body.innerHTML = '<button id="pay" type="button">Pay</button><button id="plain" type="button">No handler</button>';
    client = new PulseClient({
      endpoint: 'http://ingest.test',
      apiKey: 'pk_dev_local_0123456789abcdef',
      spanExporter: exporter,
      autocapture: { webVitals: false, errors: false, documentLoad: false },
    });
    pushed = [];
    const orig = client.events.push.bind(client.events);
    client.events.push = (e) => {
      pushed.push(e);
      orig(e);
    };
    client.start();
    // The app registers its handler after init, as real apps do.
    document.getElementById('pay')!.addEventListener('click', () => {
      void fetch('/api/pay', { method: 'POST' });
    });
  });
  afterAll(() => client.shutdown());

  it('records exactly one click span per click and links the autocapture event to it', async () => {
    document.getElementById('pay')!.click();
    await new Promise((r) => setTimeout(r, 700));
    await client.flush();
    const spans = exporter.getFinishedSpans();
    const clicks = spans.filter((s) => s.name === 'click');
    expect(clicks).toHaveLength(1);

    const fetchSpan = spans.find((s) => s.name.startsWith('HTTP POST') || s.name === 'POST');
    expect(fetchSpan?.spanContext().traceId).toBe(clicks[0].spanContext().traceId);

    const ev = pushed.find((e) => e.event === '$autocapture')!;
    expect(ev.trace_id).toBe(clicks[0].spanContext().traceId);
    expect(ev.span_id).toBe(clicks[0].spanContext().spanId);
  });

  it('leaves clicks with no app handler untraced instead of inventing a trace', async () => {
    pushed.length = 0;
    document.getElementById('plain')!.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(pushed[0].event).toBe('$autocapture');
    expect(pushed[0].trace_id).toBeUndefined();
  });

  it('gives page views a trace id, including SPA route changes', async () => {
    pushed.length = 0;
    history.pushState({}, '', '/pricing');
    await Promise.resolve();
    await Promise.resolve();
    const pv = pushed.find((e) => e.event === '$pageview')!;
    expect(pv.trace_id).toMatch(/^[0-9a-f]{32}$/);
    await client.flush();
    expect(exporter.getFinishedSpans().some((s) => s.name === 'pageview' && s.spanContext().traceId === pv.trace_id)).toBe(true);
  });
});
