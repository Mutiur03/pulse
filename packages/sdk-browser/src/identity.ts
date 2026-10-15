import { storage, uuid } from './storage';

const KEY = 'pulse_id';

interface Stored {
  anon: string;
  user?: string;
}

/** Tracks the persisted anonymous id and the optional identified user id. */
export class Identity {
  private state: Stored;

  constructor() {
    this.state = this.load() ?? { anon: uuid() };
    this.save();
  }

  get anonymousId(): string {
    return this.state.anon;
  }

  get userId(): string | undefined {
    return this.state.user;
  }

  /** The id events are attributed to: the user id once identified. */
  get distinctId(): string {
    return this.state.user ?? this.state.anon;
  }

  /** Returns the previous distinct id when it changed, so callers can alias. */
  identify(userId: string): string | undefined {
    if (!userId || userId === this.state.user) return undefined;
    const previous = this.distinctId;
    this.state.user = userId;
    this.save();
    return previous;
  }

  /** Forget the user and start a fresh anonymous id (e.g. on logout). */
  reset(): void {
    this.state = { anon: uuid() };
    this.save();
  }

  private load(): Stored | null {
    try {
      const v = JSON.parse(storage.get(KEY) ?? 'null');
      return v && typeof v.anon === 'string' ? v : null;
    } catch {
      return null;
    }
  }

  private save(): void {
    storage.set(KEY, JSON.stringify(this.state));
  }
}
