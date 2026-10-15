import { storage } from './storage';

export interface TransportOptions {
  url: string;
  storageKey: string;
  batchSize?: number;
  flushIntervalMs?: number;
  maxQueueSize?: number;
  maxBackoffMs?: number;
  fetch?: typeof fetch;
}

/**
 * Batches JSON records and posts them as an array. Items are mirrored to
 * localStorage so they survive reloads and offline periods. Failed sends are
 * retried with exponential backoff; on page hide the queue goes out with
 * sendBeacon.
 */
export class Transport<T> {
  private queue: T[];
  private timer?: ReturnType<typeof setTimeout>;
  private inflight = false;
  private backoff = 0;
  private readonly opts: Required<Omit<TransportOptions, 'fetch'>> & { fetch?: typeof fetch };

  constructor(opts: TransportOptions) {
    this.opts = {
      batchSize: 50,
      flushIntervalMs: 5000,
      maxQueueSize: 1000,
      maxBackoffMs: 60_000,
      ...opts,
    };
    this.queue = this.restore();
    if (this.queue.length) this.schedule();
  }

  get size(): number {
    return this.queue.length;
  }

  push(item: T): void {
    this.queue.push(item);
    // Drop the oldest items rather than grow without bound while offline.
    if (this.queue.length > this.opts.maxQueueSize) {
      this.queue.splice(0, this.queue.length - this.opts.maxQueueSize);
    }
    this.persist();
    if (this.queue.length >= this.opts.batchSize && !this.backoff) void this.flush();
    else this.schedule();
  }

  /** Send everything queued. Resolves when the attempt finishes. */
  async flush(): Promise<void> {
    if (this.inflight || !this.queue.length) return;
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      this.schedule();
      return;
    }
    clearTimeout(this.timer);
    this.timer = undefined;
    this.inflight = true;
    const batch = this.queue.slice(0, this.opts.batchSize);
    try {
      const f = this.opts.fetch ?? globalThis.fetch.bind(globalThis);
      const res = await f(this.opts.url, {
        method: 'POST',
        // text/plain keeps the request CORS-simple: no preflight round trip.
        headers: { 'Content-Type': 'text/plain' },
        body: JSON.stringify(batch),
        keepalive: true,
      });
      if (res.ok || (res.status >= 400 && res.status < 500 && res.status !== 429)) {
        // Success, or a permanent client error: either way drop the batch.
        this.queue.splice(0, batch.length);
        this.backoff = 0;
      } else {
        throw new Error(`HTTP ${res.status}`);
      }
    } catch {
      this.backoff = Math.min(this.backoff ? this.backoff * 2 : 1000, this.opts.maxBackoffMs);
    } finally {
      this.inflight = false;
      this.persist();
    }
    if (this.queue.length) this.schedule();
  }

  /** Best-effort send on page hide. Items stay persisted until acknowledged. */
  beacon(): void {
    if (!this.queue.length || typeof navigator === 'undefined' || !navigator.sendBeacon) return;
    const batch = this.queue.slice(0, this.opts.batchSize);
    const ok = navigator.sendBeacon(this.opts.url, new Blob([JSON.stringify(batch)], { type: 'text/plain' }));
    if (ok) {
      this.queue.splice(0, batch.length);
      this.persist();
    }
  }

  stop(): void {
    clearTimeout(this.timer);
    this.timer = undefined;
  }

  private schedule(): void {
    if (this.timer) return;
    const delay = this.backoff || this.opts.flushIntervalMs;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.flush();
    }, delay);
  }

  private persist(): void {
    if (this.queue.length) storage.set(this.opts.storageKey, JSON.stringify(this.queue));
    else storage.remove(this.opts.storageKey);
  }

  private restore(): T[] {
    try {
      const v = JSON.parse(storage.get(this.opts.storageKey) ?? '[]');
      return Array.isArray(v) ? v : [];
    } catch {
      return [];
    }
  }
}
