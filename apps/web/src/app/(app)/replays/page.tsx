'use client';
import Link from 'next/link';
import { useState } from 'react';
import { api } from '@/lib/api';
import { duration, fromRange, time } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { Badge, Card, Empty, ErrorNote, Input, Loading, PageHeader, Spinner, Td, Th, Toolbar } from '@/components/ui';
import { TimeRange } from '@/components/time-range';

export default function ReplaysPage() {
  const [q, setQ] = useQueryState(['errors', 'user', 'url', 'min', 'range']);
  const [user, setUser] = useState('');
  const [url, setUrl] = useState('');
  const range = q.range ?? '24h';
  const replays = useAsync(
    () =>
      api.listReplays({
        has_errors: q.errors === '1' || undefined,
        user: q.user || undefined,
        url: q.url || undefined,
        min_duration_ms: q.min ? Number(q.min) * 1000 : undefined,
        from: fromRange(range),
        limit: 100,
      }),
    [q.errors, q.user, q.url, q.min, range],
  );
  const list = replays.data?.replays ?? [];

  return (
    <>
      <PageHeader
        title="Session replays"
        description={`Recordings of real sessions with their errors and traces. Inputs are masked by default. Kept ${replays.data?.retention_days ?? 30} days.`}
      />
      <Toolbar>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setQ({ user, url });
          }}
          className="flex gap-2"
        >
          <Input placeholder="User id" aria-label="User" value={user || q.user || ''} onChange={(e) => setUser(e.target.value)} className="w-40" />
          <Input placeholder="Visited URL contains…" aria-label="URL" value={url || q.url || ''} onChange={(e) => setUrl(e.target.value)} className="w-56" />
          <button type="submit" className="hidden" />
        </form>
        <Input type="number" min={0} placeholder="Min s" aria-label="Min duration (s)" value={q.min ?? ''} onChange={(e) => setQ({ min: e.target.value })} className="w-24" />
        <label className="flex items-center gap-1.5 text-xs text-fg-2">
          <input type="checkbox" checked={q.errors === '1'} onChange={(e) => setQ({ errors: e.target.checked ? '1' : '' })} data-testid="has-errors" />
          With errors
        </label>
        <TimeRange value={range} onChange={(v) => setQ({ range: v })} />
        {replays.loading && <Spinner />}
      </Toolbar>
      <ErrorNote error={replays.error} />
      <Card className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="border-b border-border bg-accent/40">
            <tr>
              <Th>Session</Th>
              <Th>User</Th>
              <Th>Duration</Th>
              <Th>Pages</Th>
              <Th>Errors</Th>
              <Th>Started</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {list.map((r) => (
              <tr key={r.session_id} className="hover:bg-accent/40" data-testid="replay-row" data-session={r.session_id} data-errors={r.error_count}>
                <Td>
                  <Link href={`/replays/${r.session_id}`} className="font-medium hover:text-primary">
                    {r.urls[0]?.replace(/^https?:\/\/[^/]+/, '') || '/'}
                  </Link>
                  <div className="font-mono text-[11px] text-muted">{r.session_id.slice(0, 18)}</div>
                </Td>
                <Td className="font-mono text-xs">{r.distinct_id.slice(0, 24)}</Td>
                <Td className="font-mono text-xs">{duration(r.duration_ms)}</Td>
                <Td className="font-mono text-xs">{r.page_count}</Td>
                <Td>{r.error_count > 0 ? <Badge tone="danger">{r.error_count}</Badge> : <span className="text-xs text-muted">0</span>}</Td>
                <Td className="whitespace-nowrap text-xs text-muted">{time(r.start)}</Td>
              </tr>
            ))}
          </tbody>
        </table>
        {replays.loading && !replays.data && <Loading />}
        {!replays.loading && list.length === 0 && (
          <Empty title="No recorded sessions" action={{ label: 'Set up data collection', href: '/setup' }}>
            Enable replay in the browser SDK with init({'{'} replay: {'{'} sampleRate: 1 {'}'} {'}'}).
          </Empty>
        )}
      </Card>
    </>
  );
}
