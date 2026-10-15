'use client';
import Link from 'next/link';
import { use, useState } from 'react';
import type { IssueStatus } from '@pulse/api-client';
import { api } from '@/lib/api';
import { ago, time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, ErrorNote, PageHeader, Spinner, Td, Th } from '@/components/ui';

export default function IssuePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const detail = useAsync(() => api.getIssue(Number(id), 50), [id]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const [sel, setSel] = useState(0);
  const setStatus = async (s: IssueStatus) => {
    setBusy(true);
    try {
      await api.updateIssue(Number(id), s);
      detail.reload();
    } catch (e) {
      setErr(String(e));
    }
    setBusy(false);
  };
  const iss = detail.data?.issue;
  const events = detail.data?.recent_events ?? [];
  const ev = events[sel];

  return (
    <>
      <div className="mb-2 text-xs text-muted">
        <Link href="/issues" className="hover:text-zinc-100">
          Issues
        </Link>{' '}
        / #{id}
      </div>
      {detail.loading && !iss && <Spinner />}
      <ErrorNote error={detail.error ?? err} />
      {iss && (
        <>
          <PageHeader
            title={iss.title}
            description={`${iss.culprit}${iss.service_name ? ` · ${iss.service_name}` : ''}`}
            actions={
              <div className="flex items-center gap-2">
                <Badge tone={iss.status === 'unresolved' ? 'danger' : iss.status === 'resolved' ? 'ok' : 'neutral'} data-testid="issue-status">
                  {iss.status}
                </Badge>
                {iss.status !== 'resolved' && (
                  <Button size="sm" disabled={busy} onClick={() => setStatus('resolved')}>
                    Resolve
                  </Button>
                )}
                {iss.status !== 'ignored' && (
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => setStatus('ignored')}>
                    Ignore
                  </Button>
                )}
                {iss.status !== 'unresolved' && (
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => setStatus('unresolved')}>
                    Reopen
                  </Button>
                )}
              </div>
            }
          />
          <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-5">
            <Stat label="Events" value={iss.count.toLocaleString()} />
            <Stat label="First seen" value={ago(iss.first_seen)} />
            <Stat label="Last seen" value={ago(iss.last_seen)} />
            <Stat label="Regressions" value={String(iss.times_regressed)} />
            <Stat label="Platform" value={iss.platform} />
          </div>
          <div className="grid gap-4 xl:grid-cols-[1fr_420px]">
            <Card>
              <CardHeader
                title="Stack trace"
                description={ev ? `${ev.exception_type}: ${ev.message}` : undefined}
                actions={
                  ev?.trace_id && (
                    <Link href={`/traces/${ev.trace_id}`} className="text-xs text-primary hover:underline" data-testid="issue-trace-link">
                      View trace →
                    </Link>
                  )
                }
              />
              <pre className="max-h-[480px] overflow-auto whitespace-pre-wrap p-4 font-mono text-xs leading-relaxed text-zinc-300" data-testid="stack">
                {ev?.stack || 'No stack trace captured.'}
              </pre>
            </Card>
            <Card className="h-fit overflow-hidden">
              <CardHeader title="Occurrences" description={`Latest ${events.length}`} />
              <table className="w-full text-xs">
                <thead className="border-b border-border">
                  <tr>
                    <Th>When</Th>
                    <Th>User</Th>
                    <Th>Trace</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {events.map((e, i) => (
                    <tr key={i} role="button" tabIndex={0} onClick={() => setSel(i)} className={`hover:bg-accent/50 ${i === sel ? 'bg-accent' : ''}`}>
                      <Td className="whitespace-nowrap">{time(e.timestamp)}</Td>
                      <Td className="max-w-[120px] truncate font-mono">{e.distinct_id || '—'}</Td>
                      <Td className="font-mono">
                        {e.trace_id ? (
                          <Link href={`/traces/${e.trace_id}`} className="text-primary hover:underline" onClick={(x) => x.stopPropagation()}>
                            {e.trace_id.slice(0, 8)}
                          </Link>
                        ) : (
                          '—'
                        )}
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          </div>
        </>
      )}
    </>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <Card className="px-4 py-3">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-0.5 text-lg font-semibold">{value}</div>
    </Card>
  );
}
