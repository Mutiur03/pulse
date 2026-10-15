import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { startTracing, type Tracing } from '../src/tracing';

const calls: { url: string; headers: Record<string, string> }[] = [];

function headersOf(input: RequestInfo | URL, init?: RequestInit): Record<string, string> {
  const h = new Headers(input instanceof Request ? input.headers : init?.headers);
  if (init?.headers) new Headers(init.headers).forEach((v, k) => h.set(k, v));
  const out: Record<string, string> = {};
  h.forEach((v, k) => (out[k] = v));
  return out;
}

describe('trace header propagation', () => {
  let tracing: Tracing;

  beforeAll(() => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = input instanceof Request ? input.url : String(input);
      calls.push({ url, headers: headersOf(input, init) });
      return new Response('{}', { status: 200 });
    }) as typeof fetch;
    tracing = startTracing(
      {
        endpoint: 'http://ingest.test',
        projectId: '1',
        propagateTraceHeaderCorsUrls: [/^https:\/\/api\.allowed\.test/],
        autocapture: { documentLoad: false, userInteraction: false },
      },
      { sessionId: () => 'sess-1', distinctId: () => 'anon-1', userId: () => 'user-1' },
    );
  });
  afterAll(async () => {
    tracing.disable();
    await tracing.provider.shutdown();
  });

  const send = async (url: string) => {
    calls.length = 0;
    await fetch(url);
    return calls[0].headers;
  };

  it('adds traceparent and baggage to same-origin requests', async () => {
    const h = await send('/api/cart');
    expect(h.traceparent).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$/);
    expect(h.baggage).toContain('session.id=sess-1');
    expect(h.baggage).toContain('enduser.id=user-1');
  });

  it('adds headers to allow-listed cross-origin APIs', async () => {
    const h = await send('https://api.allowed.test/pay');
    expect(h.traceparent).toBeDefined();
  });

  it('never leaks headers to other origins', async () => {
    const h = await send('https://third-party.example/pixel');
    expect(h.traceparent).toBeUndefined();
    expect(h.baggage).toBeUndefined();
  });

  it('does not trace the SDK uploads to the ingest endpoint', async () => {
    const h = await send('http://ingest.test/v1/events');
    expect(h.traceparent).toBeUndefined();
  });
});
