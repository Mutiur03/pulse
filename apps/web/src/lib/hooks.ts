'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import { handleAuthError } from './api';

export interface Async<T> {
  data?: T;
  error?: string;
  loading: boolean;
  reload: () => void;
}

/** Runs fn whenever deps change and keeps the latest result. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): Async<T> {
  const [state, setState] = useState<{ data?: T; error?: string; loading: boolean }>({ loading: true });
  const seq = useRef(0);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(fn, deps);
  const load = useCallback(() => {
    const id = ++seq.current;
    setState((s) => ({ ...s, loading: true, error: undefined }));
    run().then(
      (data) => id === seq.current && setState({ data, loading: false }),
      (e: unknown) => {
        handleAuthError(e);
        if (id === seq.current) setState({ loading: false, error: e instanceof Error ? e.message : String(e) });
      },
    );
  }, [run]);
  useEffect(load, [load]);
  return { ...state, reload: load };
}

/** State mirrored into the URL query string so views can be shared. */
export function useQueryState(keys: string[]): [Record<string, string>, (p: Record<string, string>) => void] {
  const read = () => {
    if (typeof window === 'undefined') return {};
    const q = new URLSearchParams(window.location.search);
    return Object.fromEntries(keys.map((k) => [k, q.get(k) ?? '']).filter(([, v]) => v));
  };
  const [state, setState] = useState<Record<string, string>>({});
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => setState(read()), []);
  const update = (patch: Record<string, string>) => {
    const next = { ...state, ...patch };
    for (const k of Object.keys(next)) if (!next[k]) delete next[k];
    setState(next);
    const qs = new URLSearchParams(next).toString();
    window.history.replaceState(null, '', `${window.location.pathname}${qs ? `?${qs}` : ''}`);
  };
  return [state, update];
}
