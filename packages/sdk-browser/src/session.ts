import { storage, uuid } from './storage';

const KEY = 'pulse_session';
export const DEFAULT_SESSION_TIMEOUT = 30 * 60 * 1000;

interface Stored {
  id: string;
  start: number;
  last: number;
}

/**
 * A session ends after `timeoutMs` without activity. The state is kept in
 * localStorage so all tabs on the origin share one session.
 */
export class Session {
  private onRotate?: (id: string) => void;

  constructor(
    private timeoutMs = DEFAULT_SESSION_TIMEOUT,
    private now: () => number = Date.now,
  ) {}

  setRotateListener(fn: (id: string) => void): void {
    this.onRotate = fn;
  }

  /** Current session id without counting as activity. */
  peek(): string {
    const s = this.load();
    if (s && this.now() - s.last < this.timeoutMs) return s.id;
    return this.touch();
  }

  /** Record activity and return the session id, rotating it if it expired. */
  touch(): string {
    const t = this.now();
    let s = this.load();
    let rotated = false;
    if (!s || t - s.last >= this.timeoutMs) {
      s = { id: uuid(), start: t, last: t };
      rotated = true;
    } else {
      s.last = t;
    }
    storage.set(KEY, JSON.stringify(s));
    if (rotated) this.onRotate?.(s.id);
    return s.id;
  }

  reset(): string {
    storage.remove(KEY);
    return this.touch();
  }

  private load(): Stored | null {
    try {
      const v = JSON.parse(storage.get(KEY) ?? 'null');
      return v && typeof v.id === 'string' ? v : null;
    } catch {
      return null;
    }
  }
}
