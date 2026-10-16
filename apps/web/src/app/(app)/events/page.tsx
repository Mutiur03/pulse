'use client';
import Link from 'next/link';
import { Fragment, useState } from 'react';
import { api } from '@/lib/api';
import { fromRange, time } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { Badge, Button, Card, Empty, ErrorNote, Input, Loading, PageHeader, Select, Spinner, Td, Th } from '@/components/ui';
import { TimeRange } from '@/components/time-range';

export default function EventsPage() {
  const [q, setQ] = useQueryState(['event', 'distinct_id', 'session_id', 'range']);
  const range = q.range ?? '24h';
  const [user, setUser] = useState<string>();
  const [session, setSession] = useState<string>();
  const names = useAsync(() => api.listEventNames({ from: fromRange(range) }), [range]);
  const events = useAsync(
    () =>
      api.listEvents({
        event: q.event || undefined,
        distinct_id: q.distinct_id || undefined,
        session_id: q.session_id || undefined,
        from: fromRange(range),
        limit: 200,
      }),
    [q.event, q.distinct_id, q.session_id, range],
  );
  const [open, setOpen] = useState<number>();
  const list = events.data?.events ?? [];

  return (
    <>
      <PageHeader title="Events" description="Raw product events. Click a user or session to filter by it." />
      <Card className="mb-4 flex flex-wrap items-center gap-2 p-3">
        <Select aria-label="Event" value={q.event ?? ''} onChange={(e) => setQ({ event: e.target.value })}>
          <option value="">All events</option>
          {names.data?.map((n) => (
            <option key={n.name} value={n.name}>
              {n.name} ({n.count})
            </option>
          ))}
        </Select>
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setQ({ distinct_id: user ?? q.distinct_id ?? '', session_id: session ?? q.session_id ?? '' });
          }}
        >
          <Input placeholder="User (distinct_id)" value={user ?? q.distinct_id ?? ''} onChange={(e) => setUser(e.target.value)} />
          <Input placeholder="Session id" value={session ?? q.session_id ?? ''} onChange={(e) => setSession(e.target.value)} />
          <Button type="submit" variant="outline">
            Filter
          </Button>
        </form>
        <TimeRange value={range} onChange={(v) => setQ({ range: v })} />
        {(q.distinct_id || q.session_id || q.event) && (
          <Button
            variant="ghost"
            onClick={() => {
              setUser('');
              setSession('');
              setQ({ event: '', distinct_id: '', session_id: '' });
            }}
          >
            Clear
          </Button>
        )}
        {events.loading && <Spinner />}
      </Card>
      <ErrorNote error={events.error} onRetry={events.reload} />
      <Card className="overflow-hidden">
        <table className="w-full text-sm">
          <thead className="border-b border-border bg-accent/40">
            <tr>
              <Th>Event</Th>
              <Th>User</Th>
              <Th>Session</Th>
              <Th>Trace</Th>
              <Th>Time</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {list.map((e, i) => (
              <Fragment key={i}>
                <tr role="button" tabIndex={0} className="hover:bg-accent/40" onClick={() => setOpen(open === i ? undefined : i)} data-testid="event-row">
                  <Td>
                    <Badge tone={e.event.startsWith('$') ? 'neutral' : 'backend'}>{e.event}</Badge>
                  </Td>
                  <Td className="font-mono text-xs">
                    <button
                      className="hover:text-primary"
                      onClick={(x) => {
                        x.stopPropagation();
                        setUser(e.distinct_id);
                        setQ({ distinct_id: e.distinct_id });
                      }}
                    >
                      {e.distinct_id.slice(0, 18)}
                    </button>
                    {e.distinct_id && (
                      <Link
                        href={`/persons/profile?id=${encodeURIComponent(e.distinct_id)}`}
                        onClick={(x) => x.stopPropagation()}
                        className="ml-2 text-[11px] text-primary hover:underline"
                        data-testid="event-person-link"
                      >
                        profile
                      </Link>
                    )}
                  </Td>
                  <Td className="font-mono text-xs">
                    <button
                      className="hover:text-primary"
                      onClick={(x) => {
                        x.stopPropagation();
                        setSession(e.session_id);
                        setQ({ session_id: e.session_id });
                      }}
                    >
                      {e.session_id.slice(0, 18)}
                    </button>
                  </Td>
                  <Td className="font-mono text-xs">
                    {e.trace_id ? (
                      <Link href={`/traces/${e.trace_id}`} className="text-primary hover:underline" onClick={(x) => x.stopPropagation()}>
                        {e.trace_id.slice(0, 8)}
                      </Link>
                    ) : (
                      '—'
                    )}
                  </Td>
                  <Td className="whitespace-nowrap text-xs text-muted">{time(e.timestamp)}</Td>
                </tr>
                {open === i && (
                  <tr>
                    <td colSpan={5} className="bg-background px-4 py-3">
                      <pre className="overflow-auto font-mono text-xs text-zinc-300">{JSON.stringify(e.properties, null, 2)}</pre>
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
        {events.loading && !events.data && <Loading />}
        {!events.loading && list.length === 0 && (
          <Empty title="No events match" action={{ label: 'Set up data collection', href: '/setup' }}>
            Events appear when the browser SDK captures page views or custom events. Try a longer time range or clear the filters.
          </Empty>
        )}
      </Card>
    </>
  );
}
