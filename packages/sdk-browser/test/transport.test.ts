import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Transport } from '../src/transport';

const ok = (_u?: unknown, _i?: RequestInit) => Promise.resolve(new Response(null, { status: 202 }));
const status = (s: number) => () => Promise.resolve(new Response(null, { status: s }));

describe('transport', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it('sends a batch as one request when the batch size is reached', async () => {
    const f = vi.fn(ok);
    const t = new Transport<number>({ url: '/e', storageKey: 'q', batchSize: 3, fetch: f });
    t.push(1);
    t.push(2);
    expect(f).not.toHaveBeenCalled();
    t.push(3);
    await vi.runAllTimersAsync();
    expect(f).toHaveBeenCalledTimes(1);
    expect(JSON.parse(f.mock.calls[0][1]!.body as string)).toEqual([1, 2, 3]);
    expect(t.size).toBe(0);
    expect(localStorage.getItem('q')).toBeNull();
  });

  it('flushes on the interval', async () => {
    const f = vi.fn(ok);
    const t = new Transport<number>({ url: '/e', storageKey: 'q', flushIntervalMs: 1000, fetch: f });
    t.push(1);
    await vi.advanceTimersByTimeAsync(999);
    expect(f).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(f).toHaveBeenCalledTimes(1);
  });

  it('retries server errors with backoff and keeps items until sent', async () => {
    const f = vi.fn().mockImplementationOnce(status(503)).mockImplementationOnce(() => Promise.reject(new TypeError('offline'))).mockImplementation(ok);
    const t = new Transport<number>({ url: '/e', storageKey: 'q', flushIntervalMs: 10, fetch: f });
    t.push(1);
    await vi.advanceTimersByTimeAsync(10); // attempt 1: 503
    expect(t.size).toBe(1);
    await vi.advanceTimersByTimeAsync(999);
    expect(f).toHaveBeenCalledTimes(1); // waiting 1s backoff
    await vi.advanceTimersByTimeAsync(1); // attempt 2: network error
    expect(t.size).toBe(1);
    await vi.advanceTimersByTimeAsync(2000); // attempt 3 after 2s
    expect(f).toHaveBeenCalledTimes(3);
    expect(t.size).toBe(0);
  });

  it('drops a batch the server rejects as invalid', async () => {
    const f = vi.fn(status(400));
    const t = new Transport<number>({ url: '/e', storageKey: 'q', fetch: f });
    t.push(1);
    await t.flush();
    expect(t.size).toBe(0);
  });

  it('persists the queue so a reload (or offline period) does not lose data', async () => {
    const f = vi.fn(ok);
    const a = new Transport<number>({ url: '/e', storageKey: 'q', fetch: vi.fn(status(500)) });
    a.push(7);
    a.stop();
    const b = new Transport<number>({ url: '/e', storageKey: 'q', fetch: f });
    expect(b.size).toBe(1);
    await b.flush();
    expect(JSON.parse(f.mock.calls[0][1]!.body as string)).toEqual([7]);
  });

  it('waits while offline', async () => {
    const f = vi.fn(ok);
    const spy = vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
    const t = new Transport<number>({ url: '/e', storageKey: 'q', fetch: f });
    t.push(1);
    await t.flush();
    expect(f).not.toHaveBeenCalled();
    spy.mockReturnValue(true);
    await t.flush();
    expect(f).toHaveBeenCalledTimes(1);
  });

  it('caps the offline queue by dropping the oldest items', () => {
    const t = new Transport<number>({ url: '/e', storageKey: 'q', maxQueueSize: 2, batchSize: 10, fetch: vi.fn(ok) });
    t.push(1);
    t.push(2);
    t.push(3);
    expect(JSON.parse(localStorage.getItem('q')!)).toEqual([2, 3]);
  });

  it('uses sendBeacon on page hide', () => {
    const beacon = vi.fn(() => true);
    Object.defineProperty(navigator, 'sendBeacon', { value: beacon, configurable: true });
    const t = new Transport<number>({ url: '/e', storageKey: 'q', fetch: vi.fn(ok) });
    t.push(1);
    t.beacon();
    expect(beacon).toHaveBeenCalledWith('/e', expect.any(Blob));
    expect(t.size).toBe(0);
  });
});
