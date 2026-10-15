import { beforeEach, describe, expect, it } from 'vitest';
import { Identity } from '../src/identity';

describe('identity', () => {
  beforeEach(() => localStorage.clear());

  it('persists the anonymous id across loads', () => {
    const a = new Identity();
    expect(a.distinctId).toBe(a.anonymousId);
    expect(new Identity().anonymousId).toBe(a.anonymousId);
  });

  it('identify switches distinct id and reports the previous one once', () => {
    const id = new Identity();
    const anon = id.anonymousId;
    expect(id.identify('user_42')).toBe(anon);
    expect(id.distinctId).toBe('user_42');
    expect(id.identify('user_42')).toBeUndefined();
    expect(new Identity().distinctId).toBe('user_42');
  });

  it('reset forgets the user and issues a new anonymous id', () => {
    const id = new Identity();
    const anon = id.anonymousId;
    id.identify('u');
    id.reset();
    expect(id.userId).toBeUndefined();
    expect(id.anonymousId).not.toBe(anon);
    expect(new Identity().distinctId).toBe(id.anonymousId);
  });
});
