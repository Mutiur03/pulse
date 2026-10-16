'use client';
import { useEffect, useRef, useState } from 'react';
import type { LogRecord } from '@pulse/api-client';
import { api } from './api';
import { fromRange } from './format';

const pageSize = 100;
const tailEvery = 2000;
const tailMax = 1000;

interface LogStream {
  logs: LogRecord[];
  /** Pagination token for older logs; undefined when there are none. */
  next?: string;
  /** The search as the server normalised it. */
  query: string;
  error?: string;
  loading: boolean;
  /** Appends the next page of older logs. */
  older: () => Promise<void>;
}

/**
 * Logs matching q in the time range: the first page on every search, newer
 * logs polled while live, and older pages on demand. Responses for an
 * earlier search are dropped.
 */
export function useLogStream(q: string, range: string, live: boolean): LogStream {
  const [logs, setLogs] = useState<LogRecord[]>([]);
  const [next, setNext] = useState<string>();
  const [query, setQuery] = useState('');
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);

  // First page of a search.
  useEffect(() => {
    const id = ++seq.current;
    setLoading(true);
    setError(undefined);
    api.searchLogs({ q: q || undefined, from: fromRange(range), limit: pageSize }).then(
      (r) => {
        if (id !== seq.current) return;
        setLogs(r.logs);
        setNext(r.next_before ?? undefined);
        setQuery(r.query);
        setLoading(false);
      },
      (e: unknown) => {
        if (id !== seq.current) return;
        setError(e instanceof Error ? e.message : String(e));
        setLogs([]);
        setNext(undefined);
        setLoading(false);
      },
    );
  }, [q, range]);

  // Live tail: poll for logs newer than the newest one shown.
  const newest = useRef<string | undefined>(undefined);
  useEffect(() => {
    newest.current = logs[0]?.timestamp;
  }, [logs]);
  useEffect(() => {
    if (!live) return;
    let stop = false;
    const tick = async () => {
      const id = seq.current;
      try {
        const r = await api.searchLogs({ q: q || undefined, from: fromRange(range), after: newest.current, limit: 500 });
        if (!stop && id === seq.current && r.logs.length) setLogs((old) => [...r.logs, ...old].slice(0, tailMax));
      } catch {
        // Keep tailing; the next poll retries.
      }
    };
    const t = setInterval(tick, tailEvery);
    return () => {
      stop = true;
      clearInterval(t);
    };
  }, [live, q, range]);

  const older = async () => {
    if (!next) return;
    const id = seq.current;
    const r = await api.searchLogs({ q: q || undefined, from: fromRange(range), before: next, limit: pageSize });
    if (id !== seq.current) return;
    setLogs((old) => [...old, ...r.logs]);
    setNext(r.next_before ?? undefined);
  };

  return { logs, next, query, error, loading, older };
}
