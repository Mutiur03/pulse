import { beforeEach, describe, expect, it } from 'vitest';
import { Session } from '../src/session';

describe('session', () => {
  beforeEach(() => localStorage.clear());

  it('keeps the same id while active and rotates after 30 minutes idle', () => {
    let now = 1_000_000;
    const s = new Session(30 * 60 * 1000, () => now);
    const rotated: string[] = [];
    s.setRotateListener((id) => rotated.push(id));

    const first = s.touch();
    now += 29 * 60 * 1000;
    expect(s.touch()).toBe(first); // activity extends the session
    now += 29 * 60 * 1000;
    expect(s.touch()).toBe(first);
    now += 30 * 60 * 1000;
    const second = s.touch();
    expect(second).not.toBe(first);
    expect(rotated).toEqual([first, second]);
  });

  it('peek does not extend the session', () => {
    let now = 0;
    const s = new Session(1000, () => now);
    const id = s.touch();
    now = 900;
    expect(s.peek()).toBe(id);
    now = 1500; // 1500ms since last touch, peek at 900 did not count
    expect(s.peek()).not.toBe(id);
  });

  it('is shared through storage across instances (tabs)', () => {
    const now = () => 5;
    const a = new Session(1000, now).touch();
    expect(new Session(1000, now).touch()).toBe(a);
  });

  it('reset starts a new session', () => {
    const s = new Session();
    const a = s.touch();
    expect(s.reset()).not.toBe(a);
  });
});
