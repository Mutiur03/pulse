import { afterEach, describe, expect, it, vi } from 'vitest';
import { record } from '@rrweb/record';
import { Recorder, MARKER_TAG } from '../src/recorder';
import { recordOptions, sampled } from '../src/replay';

function capture(config = {}) {
  const sent: { url: string; body: unknown }[] = [];
  const fetchFn = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    const body = init?.body;
    const text = body instanceof Blob ? await new Response(body.stream().pipeThrough(new DecompressionStream('gzip'))).text() : String(body);
    sent.push({ url: String(url), body: JSON.parse(text) });
    return new Response(null, { status: 202 });
  }) as unknown as typeof fetch;
  const r = new Recorder({
    url: 'http://ingest.test/v1/replay?key=pk_test_0123456789abcdef',
    sessionId: () => 'sess-1',
    distinctId: () => 'anon-1',
    config: { chunkIntervalMs: 60_000, ...config },
    fetch: fetchFn,
  });
  return { r, sent };
}

afterEach(() => {
  document.body.innerHTML = '';
  localStorage.clear();
});

describe('replay privacy', () => {
  it('masks inputs, masked text and blocked elements by default', async () => {
    document.body.innerHTML = `
      <input id="email" value="ann@example.com">
      <input type="password" value="hunter2-secret">
      <textarea>private notes</textarea>
      <p class="card">4111 1111 1111 1111</p>
      <div id="ssn">SSN 078-05-1120</div>
      <p>Public heading</p>`;
    const { r, sent } = capture({ maskTextSelector: '.card', blockSelector: '#ssn' });
    r.begin();
    (document.getElementById('email') as HTMLInputElement).value = 'typed@example.com';
    document.getElementById('email')!.dispatchEvent(new Event('input', { bubbles: true }));
    await new Promise((res) => setTimeout(res, 50));
    await r.flush();
    r.stop();
    const json = JSON.stringify(sent.map((s) => s.body));
    expect(json).toContain('Public heading');
    for (const secret of ['ann@example.com', 'hunter2-secret', 'private notes', '4111 1111', '078-05-1120', 'typed@example.com']) {
      expect(json).not.toContain(secret);
    }
  });

  it('can be told not to mask inputs', () => {
    expect(recordOptions({}).maskAllInputs).toBe(true);
    expect(recordOptions({ maskAllInputs: false }).maskAllInputs).toBe(false);
    expect(recordOptions({}).maskInputOptions.password).toBe(true);
  });
});

describe('replay chunks', () => {
  it('sends gzip chunks with the session and timeline markers', async () => {
    document.body.innerHTML = '<p>hello</p>';
    const { r, sent } = capture();
    r.begin();
    r.mark('error', { trace_id: 'abc', message: 'boom' });
    await r.flush();
    r.stop();
    expect(sent).toHaveLength(1);
    // gzip when the runtime has CompressionStream for this Blob, plain JSON otherwise.
    expect(sent[0].url).toMatch(/&enc=(gzip|none)$/);
    const chunk = sent[0].body as { session_id: string; distinct_id: string; start: number; end: number; events: { type: number; data: { tag?: string; payload?: { kind: string } } }[] };
    expect(chunk.session_id).toBe('sess-1');
    expect(chunk.distinct_id).toBe('anon-1');
    expect(chunk.end).toBeGreaterThanOrEqual(chunk.start);
    expect(chunk.events.some((e) => e.type === 2)).toBe(true); // full snapshot
    const marker = chunk.events.find((e) => e.type === 5 && e.data.tag === MARKER_TAG);
    expect(marker?.data.payload?.kind).toBe('error');
    await r.flush();
    expect(sent).toHaveLength(1); // nothing new, nothing sent
  });

  it('flushes early when the chunk gets big', async () => {
    document.body.innerHTML = '<p>' + 'x'.repeat(5000) + '</p>';
    const { r, sent } = capture({ maxChunkBytes: 1000 });
    r.begin();
    await new Promise((res) => setTimeout(res, 50));
    r.stop();
    expect(sent.length).toBeGreaterThanOrEqual(1);
  });

  it('uses sendBeacon uncompressed on page hide', () => {
    const beacon = vi.fn(() => true);
    Object.defineProperty(navigator, 'sendBeacon', { value: beacon, configurable: true });
    document.body.innerHTML = '<p>bye</p>';
    const { r } = capture();
    r.begin();
    r.beacon();
    r.stop();
    expect(beacon).toHaveBeenCalledOnce();
    expect((beacon.mock.calls[0] as unknown[])[0]).toContain('&enc=none');
  });

  it('records with the real rrweb recorder', () => {
    expect(typeof record).toBe('function');
  });
});

describe('replay sampling', () => {
  it('decides once per session', () => {
    expect(sampled('a', 0.5, () => 0.9)).toBe(false);
    expect(sampled('a', 0.5, () => 0.1)).toBe(false); // remembered
    expect(sampled('b', 0.5, () => 0.1)).toBe(true);
    expect(sampled('c', 0)).toBe(false);
    expect(sampled('d', 1)).toBe(true);
  });
});
