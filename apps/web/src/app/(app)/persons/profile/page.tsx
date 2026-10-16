'use client';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { Suspense } from 'react';
import { api } from '@/lib/api';
import { ago, duration, time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Card, CardHeader, Empty, ErrorNote, PageHeader, Spinner, Td, Th } from '@/components/ui';
import { displayName } from '@/lib/person';

function Profile() {
  const id = useSearchParams().get('id') ?? '';
  const res = useAsync(() => api.getPerson(id), [id]);
  if (res.error) return <ErrorNote error={res.error} />;
  if (!res.data) return <Spinner />;
  const { person: p, sessions, events } = res.data;
  const traits = Object.entries(p.traits);
  return (
    <>
      <PageHeader title={displayName(p)} description={`First seen ${ago(p.first_seen)} · last seen ${ago(p.last_seen)}`} />
      <div className="mb-4 grid gap-4 lg:grid-cols-3">
        <Card data-testid="person-properties">
          <CardHeader title="Properties" description="Traits from the latest identify() call" />
          <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1.5 p-4 text-sm">
            <dt className="text-muted">Person id</dt>
            <dd className="break-all font-mono text-xs">{p.id}</dd>
            {traits.map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-muted">{k}</dt>
                <dd className="break-all">{typeof v === 'string' ? v : JSON.stringify(v)}</dd>
              </div>
            ))}
            {[
              ['Country', p.country],
              ['Browser', p.browser],
              ['OS', p.os],
              ['Last URL', p.last_url],
            ]
              .filter(([, v]) => v)
              .map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-muted">{k}</dt>
                  <dd className="break-all">{v}</dd>
                </div>
              ))}
          </dl>
        </Card>
        <Card data-testid="person-ids">
          <CardHeader title="Distinct ids" description="Anonymous and identified ids merged into this person" />
          <ul className="flex flex-col gap-1 p-4 font-mono text-xs">
            {p.distinct_ids.map((d) => (
              <li key={d} className="flex items-center gap-2">
                {d}
                {d === p.id && <Badge tone="ok">identified</Badge>}
              </li>
            ))}
          </ul>
        </Card>
        <Card>
          <CardHeader title="Activity" description="Last 30 days" />
          <div className="grid grid-cols-2 gap-3 p-4 text-center">
            <div>
              <div className="text-2xl font-semibold tabular-nums">{p.sessions}</div>
              <div className="text-xs text-muted">sessions</div>
            </div>
            <div>
              <div className="text-2xl font-semibold tabular-nums">{p.events}</div>
              <div className="text-xs text-muted">events</div>
            </div>
          </div>
        </Card>
      </div>
      <Card className="mb-4">
        <CardHeader title="Sessions" description="Most recent first, with replays and errors" />
        {sessions.length === 0 ? (
          <Empty>No sessions.</Empty>
        ) : (
          <table className="w-full text-sm" data-testid="person-sessions">
            <thead className="border-b border-border">
              <tr>
                <Th>Started</Th>
                <Th>Duration</Th>
                <Th>Pages</Th>
                <Th>Entry → exit</Th>
                <Th>Errors</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {sessions.map((s) => (
                <tr key={s.session_id} data-testid="person-session">
                  <Td className="text-xs">{time(s.start)}</Td>
                  <Td className="tabular-nums">{duration(Date.parse(s.end) - Date.parse(s.start))}</Td>
                  <Td className="tabular-nums">{s.pageviews}</Td>
                  <Td className="font-mono text-xs">
                    {s.entry_path || '—'} → {s.exit_path || '—'}
                  </Td>
                  <Td>{s.errors > 0 ? <Badge tone="danger">{s.errors}</Badge> : <span className="text-muted">0</span>}</Td>
                  <Td className="text-right text-xs">
                    {s.has_replay && (
                      <Link href={`/replays/${encodeURIComponent(s.session_id)}`} className="mr-3 text-primary hover:underline" data-testid="session-replay-link">
                        Replay
                      </Link>
                    )}
                    <Link href={`/events?session_id=${encodeURIComponent(s.session_id)}&range=7d`} className="text-primary hover:underline">
                      Events
                    </Link>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <Card>
        <CardHeader title="Recent events" description="Across all of this person's ids" />
        <table className="w-full text-sm" data-testid="person-events">
          <tbody className="divide-y divide-border">
            {events.map((e, i) => (
              <tr key={i}>
                <Td className="w-48 text-xs text-muted">{time(e.timestamp)}</Td>
                <Td>
                  <Badge tone={e.event.startsWith('$') ? 'neutral' : 'backend'}>{e.event}</Badge>
                </Td>
                <Td className="font-mono text-xs text-zinc-400">{String(e.properties['$pathname'] ?? '')}</Td>
                <Td className="font-mono text-xs text-muted">{e.distinct_id}</Td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </>
  );
}

export default function PersonPage() {
  return (
    <Suspense fallback={<Spinner />}>
      <Profile />
    </Suspense>
  );
}
