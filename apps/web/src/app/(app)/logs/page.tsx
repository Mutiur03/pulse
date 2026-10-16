'use client';
import Link from 'next/link';
import { Fragment, useEffect, useRef, useState } from 'react';
import type { LogRecord, LogStats } from '@pulse/api-client';
import { api } from '@/lib/api';
import { fromRange, time } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { levelColor, levelOrder, levelTone, withTerm } from '@/lib/logs';
import { Badge, Button, Card, CardHeader, Empty, ErrorNote, Input, PageHeader, Spinner, Toolbar } from '@/components/ui';
import { TimeRange } from '@/components/time-range';

const pageSize = 100;
const tailEvery = 2000;
const tailMax = 1000;

function Histogram({ stats }: { stats: LogStats }) {
  const max = Math.max(1, ...stats.buckets.map((b) => Object.values(b.counts).reduce((a, c) => a + c, 0)));
  const w = 1000;
  const h = 90;
  const bw = w / Math.max(1, stats.buckets.length);
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="h-24 w-full" preserveAspectRatio="none" role="img" aria-label="log volume" data-testid="logs-histogram">
      {stats.buckets.map((b, i) => {
        let y = h;
        return (
          <g key={b.start}>
            <title>{`${time(b.start)}: ${Object.entries(b.counts).map(([k, v]) => `${v} ${k.toLowerCase()}`).join(', ') || 'no logs'}`}</title>
            {[...levelOrder].reverse().map((lvl) => {
              const n = b.counts[lvl] ?? 0;
              if (!n) return null;
              const bh = (n / max) * (h - 4);
              y -= bh;
              return <rect key={lvl} x={i * bw + 0.5} y={y} width={Math.max(1, bw - 1)} height={bh} fill={levelColor[lvl] ?? levelColor.NONE} />;
            })}
          </g>
        );
      })}
    </svg>
  );
}

function Facet({ title, values, onPick }: { title: string; values: { value: string; count: number }[]; onPick: (v: string) => void }) {
  return (
    <div>
      <div className="mb-1 text-[11px] uppercase tracking-wide text-muted">{title}</div>
      {values.length === 0 && <div className="text-xs text-muted">none</div>}
      <ul className="space-y-0.5">
        {values.map((v) => (
          <li key={v.value}>
            <button className="flex w-full justify-between rounded px-1.5 py-0.5 text-left text-xs hover:bg-accent/60" onClick={() => onPick(v.value)} data-testid={`facet-${title.toLowerCase()}`}>
              <span className="truncate">{v.value || '(none)'}</span>
              <span className="tabular-nums text-muted">{v.count}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function LogRow({ l, open, onToggle, onFilter }: { l: LogRecord; open: boolean; onToggle: () => void; onFilter: (field: string, value: string) => void }) {
  const attrs = Object.entries(l.attributes ?? {});
  return (
    <>
      <tr role="button" tabIndex={0} className="align-top hover:bg-accent/40" onClick={onToggle} data-testid="log-row">
        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-[11px] text-muted">{time(l.timestamp)}</td>
        <td className="px-2 py-1.5">
          <Badge tone={levelTone(l.severity)}>{l.severity || '—'}</Badge>
        </td>
        <td className="whitespace-nowrap px-2 py-1.5 text-xs text-muted">{l.service_name}</td>
        <td className="w-full px-2 py-1.5 font-mono text-xs text-fg" data-testid="log-body">
          {l.body}
          {attrs.length > 0 && !open && (
            <span className="ml-2 text-muted">
              {attrs
                .slice(0, 3)
                .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
                .join(' ')}
            </span>
          )}
        </td>
        <td className="whitespace-nowrap px-3 py-1.5 text-right text-xs">
          {l.trace_id && (
            <Link href={`/traces/${l.trace_id}`} className="text-primary hover:underline" onClick={(e) => e.stopPropagation()} data-testid="log-trace-link">
              trace
            </Link>
          )}
        </td>
      </tr>
      {open && (
        <tr data-testid="log-detail">
          <td colSpan={5} className="bg-raised px-4 py-3">
            <table className="text-xs">
              <tbody>
                {[
                  ['service', l.service_name],
                  ['level', l.severity],
                  ['trace', l.trace_id ?? ''],
                  ['span', l.span_id ?? ''],
                ]
                  .filter(([, v]) => v)
                  .map(([k, v]) => (
                    <tr key={k}>
                      <td className="pr-4 text-muted">{k}</td>
                      <td className="font-mono">
                        <button className="hover:text-primary" title="Filter by this value" onClick={() => onFilter(k, k === 'level' ? v.toLowerCase() : v)}>
                          {v}
                        </button>
                      </td>
                    </tr>
                  ))}
                {attrs.map(([k, v]) => {
                  const s = typeof v === 'string' ? v : JSON.stringify(v);
                  return (
                    <tr key={k}>
                      <td className="pr-4 text-muted">{k}</td>
                      <td className="font-mono">
                        <button className="hover:text-primary" title="Filter by this value" onClick={() => onFilter(k, s)} data-testid="log-attr">
                          {s}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {l.trace_id && (
              <div className="mt-2 flex gap-3 text-xs">
                <Link href={`/traces/${l.trace_id}`} className="text-primary hover:underline">
                  Open trace →
                </Link>
                <button className="text-primary hover:underline" onClick={() => onFilter('trace', l.trace_id!)}>
                  All logs of this trace
                </button>
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

export default function LogsPage() {
  const [p, setP] = useQueryState(['q', 'range']);
  const range = p.range ?? '1h';
  const q = p.q ?? '';
  const [draft, setDraft] = useState<string>();
  const [live, setLive] = useState(false);
  const [logs, setLogs] = useState<LogRecord[]>([]);
  const [next, setNext] = useState<string>();
  const [query, setQuery] = useState('');
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState<string>();
  const seq = useRef(0);
  const stats = useAsync(() => api.logStats({ q: q || undefined, from: fromRange(range) }), [q, range, live]);

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
  newest.current = logs[0]?.timestamp;
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
  const search = (s: string) => {
    setDraft(undefined);
    setP({ q: s.trim() });
  };
  const filter = (field: string, value: string) => search(withTerm(q, field, value));

  return (
    <>
      <PageHeader
        title="Logs"
        description="Search logs from every service. Logs sent with a trace context link to their trace."
        actions={
          <Link href={`/alerts/new?log_query=${encodeURIComponent(query)}`} className="text-xs text-primary hover:underline" data-testid="logs-alert-link">
            Alert on this search →
          </Link>
        }
      />
      <Toolbar>
        <form
          className="flex min-w-[24rem] flex-1 gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            search(draft ?? q);
          }}
        >
          <Input
            aria-label="Search logs"
            className="flex-1 font-mono"
            placeholder='level:error+ service:api "payment failed" order_id:42 -health'
            value={draft ?? q}
            onChange={(e) => setDraft(e.target.value)}
          />
          <Button type="submit">Search</Button>
        </form>
        <TimeRange value={range} onChange={(v) => setP({ range: v })} />
        <Button variant={live ? 'default' : 'outline'} onClick={() => setLive(!live)} data-testid="live-tail" data-live={live} aria-pressed={live}>
          {live ? (
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-2 animate-pulse rounded-full bg-ok" /> Live
            </span>
          ) : (
            'Live tail'
          )}
        </Button>
        {(loading || stats.loading) && <Spinner />}
      </Toolbar>
      <ErrorNote error={error ?? stats.error} />
      <div className="grid gap-4 lg:grid-cols-[14rem_1fr]">
        <Card className="h-fit space-y-4 p-3" data-testid="logs-facets">
          <Facet title="Level" values={stats.data?.levels ?? []} onPick={(v) => filter('level', (v || 'none').toLowerCase())} />
          <Facet title="Service" values={stats.data?.services ?? []} onPick={(v) => filter('service', v)} />
        </Card>
        <div className="min-w-0 space-y-4">
          <Card>
            <CardHeader title="Volume" description={stats.data ? `${stats.data.total} logs` : undefined} />
            <div className="px-3 pb-2">{stats.data && <Histogram stats={stats.data} />}</div>
          </Card>
          <Card className="overflow-x-auto">
            {logs.length === 0 && !loading ? (
              q ? (
                <Empty title="No logs match this search">Try a shorter query or a longer time range.</Empty>
              ) : (
                <Empty title="No logs yet" action={{ label: 'Set up data collection', href: '/setup' }}>
                  Point an OpenTelemetry logs exporter at the ingest gateway (/v1/logs).
                </Empty>
              )
            ) : (
              <table className="w-full text-sm" data-testid="logs-table">
                <tbody className="divide-y divide-border">
                  {logs.map((l, i) => {
                    const key = `${l.timestamp}|${i}`;
                    return (
                      <Fragment key={key}>
                        <LogRow l={l} open={open === key} onToggle={() => setOpen(open === key ? undefined : key)} onFilter={filter} />
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            )}
            {next && !live && (
              <div className="border-t border-border p-2 text-center">
                <Button variant="ghost" onClick={older} data-testid="logs-older">
                  Load older
                </Button>
              </div>
            )}
          </Card>
        </div>
      </div>
    </>
  );
}
