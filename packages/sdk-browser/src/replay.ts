import type { ReplayConfig } from './config';
import { storage } from './storage';

const KEY = 'pulse_replay';

/**
 * Whether this session is recorded. The decision is made once per session and
 * stored, so every page and tab of a session agrees.
 */
export function sampled(sessionId: string, rate = 1, random: () => number = Math.random): boolean {
  try {
    const v = JSON.parse(storage.get(KEY) ?? 'null');
    if (v && v.id === sessionId && typeof v.on === 'boolean') return v.on;
  } catch {
    /* fall through */
  }
  const on = rate >= 1 || (rate > 0 && random() < rate);
  storage.set(KEY, JSON.stringify({ id: sessionId, on }));
  return on;
}

/** rrweb options derived from the config, with privacy-first defaults. */
export function recordOptions(c: ReplayConfig) {
  return {
    maskAllInputs: c.maskAllInputs ?? true,
    maskInputOptions: { password: true },
    maskTextSelector: c.maskTextSelector,
    blockSelector: c.blockSelector,
    // Our own opt-out markers apply to replay too.
    blockClass: 'pulse-block',
    maskTextClass: 'pulse-mask',
    ignoreClass: 'pulse-no-capture',
    // A fresh full snapshot now and then keeps seeking cheap.
    checkoutEveryNms: 5 * 60 * 1000,
    sampling: { mousemove: 50, scroll: 150, input: 'last' as const },
    recordCanvas: false,
    collectFonts: false,
  };
}

/** Marker kinds shown on the replay timeline. */
export type MarkerKind = 'error' | 'request' | 'pageview';

export interface ReplayHandle {
  mark(kind: MarkerKind, data: Record<string, unknown>): void;
  flush(): Promise<void>;
  beacon(): void;
  stop(): void;
}
