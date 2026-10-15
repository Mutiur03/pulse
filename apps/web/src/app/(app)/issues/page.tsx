'use client';
import Link from 'next/link';
import { useState } from 'react';
import type { IssueStatus } from '@pulse/api-client';
import { api } from '@/lib/api';
import { ago } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { cn } from '@/lib/cn';
import { Badge, Card, Empty, ErrorNote, Input, PageHeader, Select, Spinner, Td, Th } from '@/components/ui';
import { Sparkline } from '@/components/sparkline';

const statuses: (IssueStatus | 'all')[] = ['unresolved', 'resolved', 'ignored', 'all'];

export default function IssuesPage() {
  const [q, setQ] = useQueryState(['status', 'query', 'sort']);
  const [text, setText] = useState('');
  const status = (q.status ?? 'unresolved') as IssueStatus | 'all';
  const issues = useAsync(
    () =>
      api.listIssues({
        status: status === 'all' ? undefined : status,
        query: q.query || undefined,
        sort: (q.sort as 'last_seen' | 'first_seen' | 'count') || undefined,
        limit: 50,
      }),
    [status, q.query, q.sort],
  );
  const list = issues.data?.issues ?? [];

  return (
    <>
      <PageHeader title="Issues" description="Errors grouped by exception type and in-app stack frames." />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex rounded-md border border-border p-0.5">
          {statuses.map((s) => (
            <button
              key={s}
              onClick={() => setQ({ status: s === 'unresolved' ? '' : s })}
              className={cn('rounded px-3 py-1 text-sm capitalize text-muted', status === s && 'bg-accent text-zinc-100')}
            >
              {s}
            </button>
          ))}
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setQ({ query: text });
          }}
        >
          <Input placeholder="Search title or culprit…" value={text || q.query || ''} onChange={(e) => setText(e.target.value)} className="w-64" />
        </form>
        <Select aria-label="Sort" value={q.sort ?? 'last_seen'} onChange={(e) => setQ({ sort: e.target.value === 'last_seen' ? '' : e.target.value })}>
          <option value="last_seen">Last seen</option>
          <option value="first_seen">First seen</option>
          <option value="count">Events</option>
        </Select>
        {issues.loading && <Spinner />}
        {issues.data && <span className="text-xs text-muted">{issues.data.total} issues</span>}
      </div>
      <ErrorNote error={issues.error} />
      <Card className="overflow-hidden">
        <table className="w-full text-sm">
          <thead className="border-b border-border bg-accent/40">
            <tr>
              <Th>Issue</Th>
              <Th>Last 24h</Th>
              <Th className="text-right">Events</Th>
              <Th>Last seen</Th>
              <Th>Status</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {list.map((i) => (
              <tr key={i.id} className="hover:bg-accent/40" data-testid="issue-row">
                <Td className="max-w-0 w-1/2">
                  <Link href={`/issues/${i.id}`} className="block truncate font-medium hover:text-primary">
                    {i.title}
                  </Link>
                  <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted">
                    <Badge tone={i.platform === 'browser' ? 'browser' : 'backend'}>{i.platform}</Badge>
                    <span className="truncate">{i.culprit}</span>
                    {i.service_name && <span>· {i.service_name}</span>}
                    {i.times_regressed > 0 && <Badge tone="warn">regressed {i.times_regressed}×</Badge>}
                  </div>
                </Td>
                <Td>
                  <Sparkline values={i.trend ?? []} />
                </Td>
                <Td className="text-right font-mono">{i.count.toLocaleString()}</Td>
                <Td className="whitespace-nowrap text-xs text-muted">{ago(i.last_seen)}</Td>
                <Td>
                  <Badge tone={i.status === 'unresolved' ? 'danger' : i.status === 'resolved' ? 'ok' : 'neutral'}>{i.status}</Badge>
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
        {!issues.loading && list.length === 0 && <Empty>No {status === 'all' ? '' : status} issues.</Empty>}
      </Card>
    </>
  );
}
