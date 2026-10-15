import { afterEach, describe, expect, it } from 'vitest';
import { debugIdsFor, resetDebugIds } from '../src/debugids';

const g = globalThis as { __pulseDebugIds?: Record<string, string> };

afterEach(() => {
  delete g.__pulseDebugIds;
  resetDebugIds();
});

describe('debugIdsFor', () => {
  it('maps script URLs from registered stacks (Chrome and Firefox)', () => {
    g.__pulseDebugIds = {
      'Error\n    at https://shop.example.com/assets/main.js:1:5032\n    at https://x/y.js:1:1': 'aaaaaaaa-0000-4000-8000-000000000001',
      '@https://shop.example.com/assets/chunk-AB12.js:3:120\n': 'aaaaaaaa-0000-4000-8000-000000000002',
    };
    const stack = 'TypeError: x\n    at boom (https://shop.example.com/assets/main.js:1:100)\n    at https://shop.example.com/assets/chunk-AB12.js:3:9';
    expect(debugIdsFor(stack)).toEqual({
      'https://shop.example.com/assets/main.js': 'aaaaaaaa-0000-4000-8000-000000000001',
      'https://shop.example.com/assets/chunk-AB12.js': 'aaaaaaaa-0000-4000-8000-000000000002',
    });
  });

  it('only includes scripts in the stack', () => {
    g.__pulseDebugIds = { 'Error\n    at http://localhost:5173/dist/other.js:1:1': 'id' };
    expect(debugIdsFor('Error\n    at http://localhost:5173/dist/main.js:2:3')).toBeUndefined();
  });

  it('is a no-op without injected ids', () => {
    expect(debugIdsFor('Error\n    at http://a/b.js:1:1')).toBeUndefined();
  });
});
