'use client';
import Link from 'next/link';
import { use, useEffect, useMemo, useState } from 'react';
import { api } from '@/lib/api';
import { ms, time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { levelTone } from '@/lib/logs';
import { Badge, Card, CardHeader, ErrorNote, PageHeader, Spinner } from '@/components/ui';
import { Waterfall, flatten } from '@/components/waterfall';

export default function TracePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const trace = useAsync(() => api.getTrace(id), [id]);
  const rows = useMemo(() => (trace.data ? flatten(trace.data) : []), [trace.data]);
  const [sel, setSel] = useState<string>();
  useEffect(() => {
    if (!sel && rows.length) setSel((rows.find((r) => r.span.errors.length) ?? rows[0]).span.span_id);
  }, [rows, sel]);
  const span = rows.find((r) => r.span.span_id === sel)?.span;
  const t = trace.data;
  const session = rows.find((r) => r.span.session_id)?.span.session_id;
  // Logs sent with this trace's context, a little around the trace's time.
  const logs = useAsync(async () => {
    if (!t) return undefined;
    const start = Date.parse(t.start_time);
    const end = start + t.duration_ns / 1e6;
    return api.searchLogs({
      q: `trace:${id}`,
      from: new Date(start - 3600e3).toISOString(),
      to: new Date(end + 3600e3).toISOString(),
      limit: 500,
    });
  }, [t, id]);
  const traceLogs = [...(logs.data?.logs ?? [])].reverse();
  const spanName = (sid?: string) => rows.find((r) => r.span.span_id === sid)?.span.name;
  const spanLogs = traceLogs.filter((l) => l.span_id && l.span_id === sel);

  return (
    <>
      <div className="mb-2 text-xs text-muted">
        <Link href="/traces" className="hover:text-zinc-100">
          Traces
        </Link>{' '}
        / <span className="font-mono">{id}</span>
      </div>
      <PageHeader
        title={t?.roots[0]?.name ?? 'Trace'}
        actions={
          t && (
            <div className="flex flex-wrap items-center gap-3 text-xs text-muted">
              <span>
                <span className="text-zinc-100">{ms(t.duration_ns)}</span> total
              </span>
              <span>
                <span className="text-zinc-100">{t.span_count}</span> spans
              </span>
              <span className={t.error_count ? 'text-red-300' : ''}>{t.error_count} errors</span>
              <span>{time(t.start_time)}</span>
              {session && (
                <Link href={`/replays/${session}?t=${Date.parse(t.start_time)}`} className="text-primary hover:underline" data-testid="trace-replay-link">
                  Watch replay →
                </Link>
              )}
            </div>
          )
        }
      />
      <ErrorNote error={trace.error} />
      {trace.loading && <Spinner />}
      {t && (
        <div className="grid gap-4 xl:grid-cols-[1fr_380px]">
          <Card className="overflow-hidden">
            <CardHeader
              title="Waterfall"
              description="Browser → backend → database, by start time"
              actions={
                <div className="flex gap-2 text-[11px] text-muted">
                  <Legend c="bg-browser" l="browser" />
                  <Legend c="bg-backend" l="backend" />
                  <Legend c="bg-database" l="database" />
                  <Legend c="bg-red-400" l="error" />
                  <Legend c="bg-emerald-400" l="event" />
                </div>
              }
            />
            <Waterfall trace={t} selected={sel} onSelect={setSel} />
            {t.events.length > 0 && (
              <div className="border-t border-border px-4 py-3 text-xs text-muted">
                {t.events.length} product event(s) on this trace without a received span: {t.events.map((e) => e.event).join(', ')}
              </div>
            )}
            <div className="border-t border-border" data-testid="trace-logs">
              <div className="flex items-center justify-between px-4 py-2">
                <div className="text-sm font-medium">
                  Logs <span className="text-xs font-normal text-muted">{traceLogs.length}</span>
                </div>
                <Link href={`/logs?q=${encodeURIComponent(`trace:${id}`)}&range=7d`} className="text-xs text-primary hover:underline" data-testid="trace-logs-link">
                  Open in logs →
                </Link>
              </div>
              {traceLogs.length === 0 ? (
                <div className="px-4 pb-3 text-xs text-muted">{logs.loading ? 'Loading…' : 'No logs were sent with this trace.'}</div>
              ) : (
                <table className="w-full text-xs">
                  <tbody className="divide-y divide-border">
                    {traceLogs.map((l, i) => (
                      <tr
                        key={i}
                        className={`align-top ${l.span_id && l.span_id === sel ? 'bg-accent/50' : 'hover:bg-accent/30'}`}
                        onClick={() => l.span_id && spanName(l.span_id) && setSel(l.span_id)}
                        data-testid="trace-log"
                      >
                        <td className="whitespace-nowrap px-4 py-1 font-mono text-[11px] text-muted">+{ms((Date.parse(l.timestamp) - Date.parse(t.start_time)) * 1e6)}</td>
                        <td className="px-2 py-1">
                          <Badge tone={levelTone(l.severity)}>{l.severity || '—'}</Badge>
                        </td>
                        <td className="w-full px-2 py-1 font-mono text-zinc-200">{l.body}</td>
                        <td className="whitespace-nowrap px-4 py-1 text-right text-muted">{spanName(l.span_id) ?? l.service_name}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </Card>
          {span && (
            <Card className="h-fit xl:sticky xl:top-6" data-testid="span-panel">
              <CardHeader
                title={span.name}
                description={
                  <span className="flex items-center gap-1.5">
                    <Badge tone={span.source}>{span.source}</Badge> {span.service_name} · {span.kind} · {ms(span.duration_ns)}
                  </span>
                }
              />
              <div className="space-y-4 p-4 text-xs">
                {span.errors.map((e, i) => (
                  <div key={i} className="rounded-md border border-danger/40 bg-danger/10 p-3">
                    <div className="font-medium text-red-300">
                      {e.exception_type}: {e.message}
                    </div>
                    {e.issue_id > 0 && (
                      <Link href={`/issues/${e.issue_id}`} className="mt-1 inline-block text-primary hover:underline">
                        Open issue #{e.issue_id} →
                      </Link>
                    )}
                    {e.stack && <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap font-mono text-[11px] text-zinc-400">{e.stack}</pre>}
                  </div>
                ))}
                {spanLogs.length > 0 && (
                  <Section title="Logs">
                    <div className="space-y-1" data-testid="span-logs">
                      {spanLogs.map((l, i) => (
                        <div key={i} className="flex gap-2">
                          <Badge tone={levelTone(l.severity)}>{l.severity || '—'}</Badge>
                          <span className="font-mono text-zinc-200">{l.body}</span>
                        </div>
                      ))}
                    </div>
                  </Section>
                )}
                {span.events.length > 0 && (
                  <Section title="Product events">
                    {span.events.map((e, i) => (
                      <div key={i} className="flex justify-between">
                        <span className="text-emerald-300">{e.event}</span>
                        <span className="text-muted">{e.distinct_id}</span>
                      </div>
                    ))}
                  </Section>
                )}
                {(span.span_events?.length ?? 0) > 0 && (
                  <Section title="Span events">
                    {span.span_events!.map((e, i) => (
                      <div key={i}>
                        {e.name} <span className="text-muted">{time(e.time)}</span>
                      </div>
                    ))}
                  </Section>
                )}
                <Section title="Attributes">
                  <dl className="grid grid-cols-[minmax(0,40%)_1fr] gap-x-3 gap-y-1 font-mono text-[11px]">
                    {attrs(span).map(([k, v]) => (
                      <div key={k} className="contents">
                        <dt className="truncate text-muted" title={k}>
                          {k}
                        </dt>
                        <dd className="break-all text-zinc-200">{v}</dd>
                      </div>
                    ))}
                  </dl>
                </Section>
              </div>
            </Card>
          )}
        </div>
      )}
    </>
  );
}

function attrs(s: import('@pulse/api-client').SpanNode): [string, string][] {
  const base: [string, string | undefined][] = [
    ['span_id', s.span_id],
    ['parent_span_id', s.parent_span_id || undefined],
    ['status', s.status_code + (s.status_message ? ` (${s.status_message})` : '')],
    ['http.route', s.http_route],
    ['http.status', s.http_status?.toString()],
    ['db.system', s.db_system],
    ['db.statement', s.db_statement],
    ['session.id', s.session_id],
  ];
  const shown = base.filter((x): x is [string, string] => !!x[1]);
  const seen = new Set(shown.map(([k]) => k));
  const extra = Object.entries(s.attributes ?? {})
    .filter(([k]) => !seen.has(k) && !(k === 'db.system.name' || k === 'db.query.text'))
    .sort(([a], [b]) => a.localeCompare(b));
  return [...shown, ...extra];
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1.5 font-medium uppercase tracking-wide text-muted">{title}</div>
      <div className="space-y-1">{children}</div>
    </div>
  );
}

function Legend({ c, l }: { c: string; l: string }) {
  return (
    <span className="flex items-center gap-1">
      <span className={`h-2 w-2 rounded-sm ${c}`} />
      {l}
    </span>
  );
}
