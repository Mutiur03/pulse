// Loaded on demand (dynamic import) so apps without replay, or sessions that
// are not sampled, never download rrweb.
import { record } from '@rrweb/record';
import type { ReplayConfig } from './config';
import { recordOptions, type MarkerKind, type ReplayHandle } from './replay';

type RecordFn = typeof record;

export interface RecorderDeps {
  /** e.g. https://ingest.example.com/v1/replay?key=pk_... */
  url: string;
  sessionId: () => string;
  distinctId: () => string;
  config: ReplayConfig;
  record?: RecordFn;
  fetch?: typeof fetch;
  now?: () => number;
}

interface Chunk {
  session_id: string;
  distinct_id: string;
  start: number;
  end: number;
  events: unknown[];
}

export const MARKER_TAG = 'pulse';

async function gzip(text: string): Promise<Blob | undefined> {
  if (typeof CompressionStream === 'undefined') return undefined;
  try {
    const stream = new Blob([text]).stream().pipeThrough(new CompressionStream('gzip'));
    return await new Response(stream).blob();
  } catch {
    return undefined;
  }
}

/** Records the page with rrweb and ships the events in chunks. */
export class Recorder implements ReplayHandle {
  private events: unknown[] = [];
  private bytes = 0;
  private session = '';
  private start = 0;
  private timer?: ReturnType<typeof setInterval>;
  private stopFn?: () => void;
  private readonly rec: RecordFn;
  private readonly maxBytes: number;

  constructor(private deps: RecorderDeps) {
    this.rec = deps.record ?? record;
    this.maxBytes = deps.config.maxChunkBytes ?? 512 * 1024;
  }

  begin(): void {
    this.session = this.deps.sessionId();
    this.stopFn =
      this.rec({
        ...recordOptions(this.deps.config),
        emit: (e) => this.push(e),
      }) ?? undefined;
    this.timer = setInterval(() => void this.flush(), this.deps.config.chunkIntervalMs ?? 5000);
  }

  private now(): number {
    return (this.deps.now ?? Date.now)();
  }

  private push(e: unknown): void {
    const sid = this.deps.sessionId();
    if (sid !== this.session) {
      // A new session starts a new replay: ship the old one, then snapshot.
      void this.flush();
      this.session = sid;
      this.rec.takeFullSnapshot?.(true);
    }
    if (!this.events.length) this.start = (e as { timestamp?: number }).timestamp ?? this.now();
    this.events.push(e);
    this.bytes += JSON.stringify(e).length;
    if (this.bytes >= this.maxBytes) void this.flush();
  }

  mark(kind: MarkerKind, data: Record<string, unknown>): void {
    try {
      this.rec.addCustomEvent?.(MARKER_TAG, { kind, ...data });
    } catch {
      /* not recording */
    }
  }

  private take(): Chunk | undefined {
    if (!this.events.length) return undefined;
    const events = this.events;
    const last = events[events.length - 1] as { timestamp?: number };
    this.events = [];
    this.bytes = 0;
    return { session_id: this.session, distinct_id: this.deps.distinctId(), start: this.start, end: last.timestamp ?? this.now(), events };
  }

  async flush(): Promise<void> {
    const chunk = this.take();
    if (!chunk) return;
    const json = JSON.stringify(chunk);
    const gz = await gzip(json);
    const f = this.deps.fetch ?? globalThis.fetch.bind(globalThis);
    try {
      await f(`${this.deps.url}&enc=${gz ? 'gzip' : 'none'}`, {
        method: 'POST',
        // text/plain keeps the request CORS-simple even for gzip bytes.
        headers: { 'Content-Type': 'text/plain' },
        body: gz ?? json,
        keepalive: (gz?.size ?? json.length) < 60_000,
      });
    } catch {
      // Replay is best effort; a lost chunk leaves a gap, not an error.
    }
  }

  /** Page is going away: send synchronously, uncompressed. */
  beacon(): void {
    const chunk = this.take();
    if (!chunk) return;
    const body = JSON.stringify(chunk);
    const url = `${this.deps.url}&enc=none`;
    if (typeof navigator !== 'undefined' && navigator.sendBeacon?.(url, new Blob([body], { type: 'text/plain' }))) return;
    void (this.deps.fetch ?? globalThis.fetch)?.(url, { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body, keepalive: true }).catch(() => undefined);
  }

  stop(): void {
    clearInterval(this.timer);
    this.stopFn?.();
    this.stopFn = undefined;
  }
}

export function startRecorder(deps: RecorderDeps): Recorder {
  const r = new Recorder(deps);
  r.begin();
  return r;
}
