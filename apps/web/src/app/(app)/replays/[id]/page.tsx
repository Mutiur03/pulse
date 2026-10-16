'use client';
import Link from 'next/link';
import { use, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { api } from '@/lib/api';
import { duration, ms, time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { cn } from '@/lib/cn';
import { Badge, Card, CardHeader, ErrorNote, PageHeader, Spinner } from '@/components/ui';
import { ReplayPlayer, type Marker, type PlayerHandle } from '@/components/replay-player';

type RREvent = { type: number; timestamp: number; data?: { tag?: string; payload?: Record<string, unknown> } };

export default function ReplayPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const t = Number(useSearchParams().get('t')) || undefined;
  const detail = useAsync(() => api.getReplay(id), [id]);
  const events = useAsync(() => api.replayEvents(id), [id]);
  const player = useRef<PlayerHandle>(null);
  const [now, setNow] = useState<number>();
  const [tab, setTab] = useState<'errors' | 'traces'>('errors');

  const markers = useMemo<Marker[]>(() => {
    const out: Marker[] = [];
    for (const e of (events.data ?? []) as RREvent[]) {
      if (e.type !== 5 || e.data?.tag !== 'pulse') continue;
      const p = e.data.payload ?? {};
      if (p.kind === 'request') out.push({ at: e.timestamp, kind: 'request', label: String(p.name ?? 'request') });
      if (p.kind === 'pageview') out.push({ at: e.timestamp, kind: 'pageview', label: String(p.path ?? 'page') });
    }
    for (const e of detail.data?.errors ?? []) out.push({ at: Date.parse(e.timestamp), kind: 'error', label: `${e.exception_type}: ${e.message}` });
    return out;
  }, [events.data, detail.data]);

  const s = detail.data?.summary;
  const state = (iso: string) => {
    const at = Date.parse(iso);
    if (now === undefined) return 'future';
    if (Math.abs(at - now) < 2000) return 'active';
    return at < now ? 'past' : 'future';
  };

  return (
    <>
      <PageHeader
        title="Session replay"
        description={s ? `${s.distinct_id} · ${time(s.start)} · ${duration(s.duration_ms)} · ${s.page_count} page${s.page_count === 1 ? '' : 's'}` : id}
        actions={
          <Link href="/replays" className="text-sm text-muted hover:text-fg">
            All replays
          </Link>
        }
      />
      <ErrorNote error={detail.error ?? events.error} />
      <div className="grid gap-4 xl:grid-cols-[1fr_360px]">
        <Card className="p-3">
          {events.loading || !events.data ? (
            <div className="grid h-[480px] place-items-center">
              <Spinner />
            </div>
          ) : (
            <ReplayPlayer ref={player} events={events.data} markers={markers} start={t} onTime={setNow} />
          )}
          {s && s.urls.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-1 text-xs text-muted">
              {s.urls.map((u) => (
                <Badge key={u}>{u.replace(/^https?:\/\/[^/]+/, '') || '/'}</Badge>
              ))}
            </div>
          )}
        </Card>
        <Card className="flex max-h-[600px] flex-col overflow-hidden">
          <CardHeader
            title={
              <div className="flex gap-3">
                {(['errors', 'traces'] as const).map((k) => (
                  <button
                    key={k}
                    onClick={() => setTab(k)}
                    className={cn('text-sm', tab === k ? 'text-fg' : 'text-muted hover:text-fg-2')}
                    data-testid={`tab-${k}`}
                  >
                    {k === 'errors' ? `Errors (${detail.data?.errors.length ?? 0})` : `Traces (${detail.data?.traces.length ?? 0})`}
                  </button>
                ))}
              </div>
            }
          />
          <div className="flex-1 divide-y divide-border overflow-auto">
            {tab === 'errors' &&
              detail.data?.errors.map((e, i) => (
                <button
                  key={i}
                  onClick={() => player.current?.seek(Date.parse(e.timestamp) - 1500)}
                  className={cn('block w-full px-4 py-2.5 text-left text-sm hover:bg-accent/40', state(e.timestamp) === 'active' && 'bg-danger/10', state(e.timestamp) === 'future' && 'opacity-60')}
                  data-testid="replay-error"
                >
                  <div className="truncate font-medium text-danger-fg">{e.exception_type}</div>
                  <div className="truncate text-xs text-fg-2">{e.message}</div>
                  <div className="mt-0.5 flex gap-2 text-[11px] text-muted">
                    <span>{s ? duration(Date.parse(e.timestamp) - Date.parse(s.start)) : ''}</span>
                    {e.issue_id > 0 && (
                      <Link href={`/issues/${e.issue_id}`} className="hover:text-fg" onClick={(ev) => ev.stopPropagation()}>
                        issue #{e.issue_id}
                      </Link>
                    )}
                    {e.trace_id && (
                      <Link href={`/traces/${e.trace_id}`} className="hover:text-fg" onClick={(ev) => ev.stopPropagation()}>
                        trace
                      </Link>
                    )}
                  </div>
                </button>
              ))}
            {tab === 'traces' &&
              detail.data?.traces.map((tr) => (
                <button
                  key={tr.trace_id}
                  onClick={() => player.current?.seek(Date.parse(tr.start) - 500)}
                  className={cn('block w-full px-4 py-2.5 text-left text-sm hover:bg-accent/40', state(tr.start) === 'active' && 'bg-primary/10', state(tr.start) === 'future' && 'opacity-60')}
                  data-testid="replay-trace"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate font-medium">{tr.name || '(unnamed)'}</span>
                    {tr.error_count > 0 && <Badge tone="danger">error</Badge>}
                  </div>
                  <div className="mt-0.5 flex gap-2 text-[11px] text-muted">
                    <span>{tr.service_name}</span>
                    <span>{ms(tr.duration_ns)}</span>
                    <span>{tr.span_count} spans</span>
                    <Link href={`/traces/${tr.trace_id}`} className="hover:text-fg" onClick={(ev) => ev.stopPropagation()}>
                      open
                    </Link>
                  </div>
                </button>
              ))}
            {detail.data && detail.data[tab].length === 0 && <div className="p-4 text-xs text-muted">None in this session.</div>}
          </div>
        </Card>
      </div>
    </>
  );
}
