'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import { handleAuthError } from './api';

interface Async<T> {
  data?: T;
  error?: string;
  loading: boolean;
  reload: () => void;
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Keeps a ref pointing at the latest value, updated after each render. */
export function useLatest<T>(value: T) {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  });
  return ref;
}

/**
 * Runs fn on mount and whenever deps change by value, keeping the latest
 * result. Responses from superseded runs are ignored.
 */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): Async<T> {
  const [state, setState] = useState<{ data?: T; error?: string; loading: boolean }>({ loading: true });
  const latest = useLatest(fn);
  const seq = useRef(0);
  const key = JSON.stringify(deps);
  const load = useCallback(() => {
    const id = ++seq.current;
    setState((s) => ({ ...s, loading: true, error: undefined }));
    latest.current().then(
      (data) => id === seq.current && setState({ data, loading: false }),
      (e: unknown) => {
        handleAuthError(e);
        if (id === seq.current) setState({ loading: false, error: message(e) });
      },
    );
  }, [latest]);
  useEffect(() => {
    load();
  }, [key, load]);
  return { ...state, reload: load };
}

interface Runner<A extends unknown[], T> {
  data?: T;
  error?: string;
  busy: boolean;
  run: (...args: A) => Promise<void>;
}

/**
 * A query the user runs on demand (and once on mount): tracks the result,
 * error and busy state, ignores superseded runs, and calls onResult with
 * each current result.
 */
export function useRunner<A extends unknown[], T>(fn: (...args: A) => Promise<T>, onResult?: (data: T) => void): Runner<A, T> {
  const [state, setState] = useState<{ data?: T; error?: string; busy: boolean }>({ busy: false });
  const latest = useLatest({ fn, onResult });
  const seq = useRef(0);
  const run = useCallback(
    async (...args: A) => {
      const id = ++seq.current;
      setState((s) => ({ ...s, busy: true, error: undefined }));
      try {
        const data = await latest.current.fn(...args);
        if (id !== seq.current) return; // a newer run is in flight
        setState((s) => ({ ...s, data }));
        latest.current.onResult?.(data);
      } catch (e) {
        handleAuthError(e);
        if (id === seq.current) setState((s) => ({ ...s, error: message(e) }));
      }
      if (id === seq.current) setState((s) => ({ ...s, busy: false }));
    },
    [latest],
  );
  useEffect(() => {
    void run(...([] as unknown as A));
  }, [run]);
  return { ...state, run };
}

function readQuery(keys: string[]): Record<string, string> {
  if (typeof window === 'undefined') return {};
  const q = new URLSearchParams(window.location.search);
  return Object.fromEntries(keys.map((k) => [k, q.get(k) ?? '']).filter(([, v]) => v));
}

/** State mirrored into the URL query string so views can be shared. */
export function useQueryState(keys: string[]): [Record<string, string>, (p: Record<string, string>) => void] {
  const [state, setState] = useState<Record<string, string>>({});
  const keyList = keys.join(',');
  useEffect(() => setState(readQuery(keyList.split(','))), [keyList]);
  const update = (patch: Record<string, string>) => {
    const next = { ...state, ...patch };
    for (const k of Object.keys(next)) if (!next[k]) delete next[k];
    setState(next);
    const qs = new URLSearchParams(next).toString();
    window.history.replaceState(null, '', `${window.location.pathname}${qs ? `?${qs}` : ''}`);
  };
  return [state, update];
}
