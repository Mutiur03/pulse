'use client';
import Link from 'next/link';
import { useState } from 'react';
import { api } from '@/lib/api';
import { fromRange, ms, time } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { Badge, Card, Empty, ErrorNote, Input, Loading, PageHeader, Select, Spinner, Td, Th } from '@/components/ui';
import { TimeRange } from '@/components/time-range';

export default function TracesPage() {
  const [q, setQ] = useQueryState(['service', 'status', 'min', 'max', 'query', 'range']);
  const [text, setText] = useState('');
  const range = q.range ?? '24h';
  const services = useAsync(() => api.listServices({ from: fromRange(range) }), [range]);
  const traces = useAsync(
    () =>
      api.listTraces({
        service: q.service || undefined,
        status: (q.status as 'ok' | 'error') || undefined,
        min_duration_ms: q.min ? Number(q.min) : undefined,
        max_duration_ms: q.max ? Number(q.max) : undefined,
        query: q.query || undefined,
        from: fromRange(range),
        limit: 100,
      }),
    [q.service, q.status, q.min, q.max, q.query, range],
  );
  const list = traces.data?.traces ?? [];
  const maxDur = Math.max(1, ...list.map((t) => t.duration_ns));

  return (
    <>
      <PageHeader title="Traces" description="Every request from the browser through your services and database." />
      <Card className="mb-4 flex flex-wrap items-center gap-2 p-3">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setQ({ query: text });
          }}
        >
          <Input placeholder="Search span names…" value={text || q.query || ''} onChange={(e) => setText(e.target.value)} className="w-56" />
        </form>
        <Select aria-label="Service" value={q.service ?? ''} onChange={(e) => setQ({ service: e.target.value })}>
          <option value="">All services</option>
          {services.data?.map((s) => (
            <option key={s}>{s}</option>
          ))}
        </Select>
        <Select aria-label="Status" value={q.status ?? ''} onChange={(e) => setQ({ status: e.target.value })}>
          <option value="">Any status</option>
          <option value="error">Errors</option>
          <option value="ok">No errors</option>
        </Select>
        <Input type="number" min={0} placeholder="Min ms" aria-label="Min duration (ms)" value={q.min ?? ''} onChange={(e) => setQ({ min: e.target.value })} className="w-24" />
        <Input type="number" min={0} placeholder="Max ms" aria-label="Max duration (ms)" value={q.max ?? ''} onChange={(e) => setQ({ max: e.target.value })} className="w-24" />
        <TimeRange value={range} onChange={(v) => setQ({ range: v })} />
        {traces.loading && <Spinner />}
      </Card>
      <ErrorNote error={traces.error} onRetry={traces.reload} />
      <Card className="overflow-hidden">
        <table className="w-full text-sm">
          <thead className="border-b border-border bg-accent/40">
            <tr>
              <Th>Root span</Th>
              <Th>Services</Th>
              <Th className="w-64">Duration</Th>
              <Th>Spans</Th>
              <Th>Started</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {list.map((t) => (
              <tr key={t.trace_id} className="hover:bg-accent/40" data-testid="trace-row">
                <Td>
                  <Link href={`/traces/${t.trace_id}`} className="font-medium hover:text-primary">
                    {t.root_name || '(unnamed)'}
                  </Link>
                  <div className="mt-0.5 flex items-center gap-1.5">
                    <span className="font-mono text-[11px] text-muted">{t.trace_id.slice(0, 16)}</span>
                    {t.error_count > 0 && <Badge tone="danger">{t.error_count} error{t.error_count > 1 ? 's' : ''}</Badge>}
                    {t.http_status && <Badge tone={t.http_status >= 400 ? 'danger' : 'neutral'}>{t.http_status}</Badge>}
                  </div>
                </Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {t.has_browser && <Badge tone="browser">browser</Badge>}
                    {t.services.map((s) => (
                      <Badge key={s}>{s}</Badge>
                    ))}
                  </div>
                </Td>
                <Td>
                  <div className="flex items-center gap-2">
                    <div className="h-1.5 flex-1 rounded bg-zinc-800">
                      <div className={`h-1.5 rounded ${t.error_count ? 'bg-danger' : 'bg-primary'}`} style={{ width: `${Math.max(2, (t.duration_ns / maxDur) * 100)}%` }} />
                    </div>
                    <span className="w-16 text-right font-mono text-xs">{ms(t.duration_ns)}</span>
                  </div>
                </Td>
                <Td className="font-mono text-xs">{t.span_count}</Td>
                <Td className="whitespace-nowrap text-xs text-muted">{time(t.start_time)}</Td>
              </tr>
            ))}
          </tbody>
        </table>
        {traces.loading && !traces.data && <Loading />}
        {!traces.loading &&
          list.length === 0 &&
          (Object.keys(q).some((k) => k !== 'range') ? (
            <Empty title="No traces match these filters">Clear a filter or pick a longer time range.</Empty>
          ) : (
            <Empty title="No traces in this time range" action={{ label: 'Set up data collection', href: '/setup' }}>
              Traces appear when a backend or browser SDK sends spans.
            </Empty>
          ))}
      </Card>
    </>
  );
}
