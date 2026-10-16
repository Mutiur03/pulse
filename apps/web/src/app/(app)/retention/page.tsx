'use client';
import { useState } from 'react';
import type { RetentionQuery } from '@pulse/api-client';
import { api } from '@/lib/api';
import { useAsync, useRunner } from '@/lib/hooks';
import { Button, Card, CardHeader, Empty, ErrorNote, PageHeader, Select, Spinner, Toolbar } from '@/components/ui';

function cohortLabel(iso: string, period: string) {
  const d = new Date(iso);
  const s = d.toLocaleDateString([], { month: 'short', day: 'numeric', timeZone: 'UTC' });
  return period === 'week' ? `Week of ${s}` : s;
}

export default function RetentionPage() {
  const names = useAsync(() => api.listEventNames({ from: new Date(Date.now() - 30 * 86400e3).toISOString() }), []);
  const [q, setQ] = useState<RetentionQuery>({ start_event: '$pageview', return_event: '$pageview', period: 'day', periods: 7 });
  const { data: res, error, busy, run } = useRunner((query: RetentionQuery = q) => api.queryRetention(query));
  const events = Array.from(new Set([...(names.data?.map((n) => n.name) ?? []), q.start_event ?? '', q.return_event ?? ''].filter(Boolean)));
  const total = res?.cohorts.reduce((a, c) => a + c.size, 0) ?? 0;
  // Average retention per offset, weighted by cohort size, over cohorts that reached it.
  const avg = res
    ? Array.from({ length: res.periods + 1 }, (_, k) => {
        const rows = res.cohorts.filter((c) => c.values.length > k && c.size > 0);
        const size = rows.reduce((a, c) => a + c.size, 0);
        return size ? rows.reduce((a, c) => a + c.values[k], 0) / size : undefined;
      })
    : [];
  const sel = (label: string, value: string | undefined, set: (v: string) => void) => (
    <label className="flex flex-col gap-1 text-xs text-muted">
      {label}
      <Select aria-label={label} value={value ?? ''} onChange={(e) => set(e.target.value)}>
        <option value="">Any event</option>
        {events.map((e) => (
          <option key={e}>{e}</option>
        ))}
      </Select>
    </label>
  );
  return (
    <>
      <PageHeader title="Retention" description="Do people come back? Each row is the people who first did the start event in that period, and the share who returned in each later period." />
      <Toolbar className="items-end">
        {sel('Start event', q.start_event, (v) => setQ({ ...q, start_event: v }))}
        {sel('Return event', q.return_event, (v) => setQ({ ...q, return_event: v }))}
        <label className="flex flex-col gap-1 text-xs text-muted">
          Period
          <Select aria-label="Period" value={q.period} onChange={(e) => setQ({ ...q, period: e.target.value as RetentionQuery['period'], periods: e.target.value === 'week' ? 8 : 7 })}>
            <option value="day">Daily</option>
            <option value="week">Weekly</option>
          </Select>
        </label>
        <Button onClick={() => run()} disabled={busy} data-testid="run-retention" data-busy={busy}>
          {busy ? 'Running…' : 'Run'}
        </Button>
        {busy && <Spinner />}
      </Toolbar>
      <ErrorNote error={error} />
      {res && (
        <Card>
          <CardHeader title="Cohorts" description={`${total.toLocaleString()} people in ${res.cohorts.length} cohorts`} />
          {total === 0 ? (
            <Empty>Nobody did the start event in this range.</Empty>
          ) : (
            <div className="overflow-x-auto p-4">
              <table className="w-full border-separate border-spacing-0.5 text-xs" data-testid="retention-table">
                <thead>
                  <tr className="text-muted">
                    <th className="px-2 py-1 text-left font-medium">Cohort</th>
                    <th className="px-2 py-1 text-right font-medium">People</th>
                    {avg.map((_, k) => (
                      <th key={k} className="px-2 py-1 font-medium">
                        {res.period === 'week' ? 'Week' : 'Day'} {k}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  <tr data-testid="retention-average">
                    <td className="px-2 py-1.5 font-medium">Average</td>
                    <td className="px-2 py-1.5 text-right tabular-nums">{total}</td>
                    {avg.map((v, k) => (
                      <Cell key={k} rate={v} />
                    ))}
                  </tr>
                  {res.cohorts.map((c) => (
                    <tr key={c.start} data-testid="retention-row">
                      <td className="whitespace-nowrap px-2 py-1.5 text-fg-2">{cohortLabel(c.start, res.period)}</td>
                      <td className="px-2 py-1.5 text-right tabular-nums">{c.size}</td>
                      {avg.map((_, k) => (
                        <Cell key={k} rate={k < c.values.length && c.size ? c.values[k] / c.size : undefined} count={c.values[k]} />
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}
    </>
  );
}

function Cell({ rate, count }: { rate?: number; count?: number }) {
  if (rate === undefined) return <td className="rounded bg-raised/40 px-2 py-1.5" />;
  return (
    <td
      className="rounded px-2 py-1.5 text-center tabular-nums"
      style={{ background: `rgba(139, 92, 246, ${0.08 + rate * 0.8})` }}
      title={count !== undefined ? `${count} people` : undefined}
      data-testid="retention-cell"
    >
      {Math.round(rate * 100)}%
    </td>
  );
}
