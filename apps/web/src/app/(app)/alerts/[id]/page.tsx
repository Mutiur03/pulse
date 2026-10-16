'use client';
import Link from 'next/link';
import { use, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import type { AlertHistoryEntry } from '@pulse/api-client';
import { api, currentProjectId } from '@/lib/api';
import { ago, time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { span, stateLabel, stateTone, value } from '@/lib/alerts';
import { toInput } from '@/components/alert-form';
import { AlertChart } from '@/components/alert-chart';
import { Badge, Button, Card, CardHeader, Empty, ErrorNote, PageHeader, Spinner } from '@/components/ui';

const noticeLabel: Record<string, string> = { firing: 'Notified: alerting', still_firing: 'Notified: still alerting', resolved: 'Notified: resolved' };

function Entry({ e, unit }: { e: AlertHistoryEntry; unit: string }) {
  return (
    <li className="relative pb-5 pl-6 last:pb-0" data-testid="history-entry" data-state={e.state} data-notice={e.notice}>
      <span className={`absolute left-0 top-1 h-2.5 w-2.5 rounded-full ${e.state === 'alerting' ? 'bg-danger' : e.state === 'ok' ? 'bg-ok' : 'bg-warn'}`} />
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge tone={stateTone[e.state]}>{stateLabel[e.state]}</Badge>
        <span className="text-xs text-muted">from {stateLabel[e.previous]}</span>
        {e.value !== undefined && <span className="font-mono text-xs">value {value(e.value, unit)}</span>}
        <span className="text-xs text-muted">{time(e.at)}</span>
      </div>
      <div className="mt-1 flex flex-wrap gap-3 text-xs">
        {e.notice && !e.suppressed && <span className="text-fg-2">{noticeLabel[e.notice] ?? e.notice}</span>}
        {e.suppressed && <span className="text-warn-fg">Not sent ({e.suppressed})</span>}
        {e.failures && <span className="text-danger-fg">Failed: {e.failures}</span>}
        {e.message && <span className="text-muted">{e.message}</span>}
        {e.issue_id ? (
          <Link href={`/issues/${e.issue_id}`} className="text-primary hover:underline" data-testid="history-issue">
            Issue #{e.issue_id} →
          </Link>
        ) : null}
        {e.trace_id && (
          <Link href={`/traces/${e.trace_id}`} className="text-primary hover:underline" data-testid="history-trace">
            Example trace →
          </Link>
        )}
      </div>
    </li>
  );
}

export default function AlertPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const data = useAsync(async () => {
    const project = await currentProjectId();
    const detail = await api.getAlert(project, Number(id));
    const preview = await api.previewAlert(project, toInput(detail.rule)).catch(() => undefined);
    return { project, detail, preview };
  }, [id]);
  // Keep the state and history live while the page is open.
  const { reload } = data;
  useEffect(() => {
    const t = setInterval(reload, 5000);
    return () => clearInterval(t);
  }, [reload]);

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(undefined);
    try {
      await fn();
      reload();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const d = data.data;
  const r = d?.detail.rule;
  const muted = r?.muted_until && Date.parse(r.muted_until) > Date.now();

  return (
    <>
      <div className="mb-2 text-xs text-muted">
        <Link href="/alerts" className="hover:text-fg">
          Alerts
        </Link>{' '}
        / <span>{r?.name ?? id}</span>
      </div>
      <PageHeader
        title={r?.name ?? 'Alert rule'}
        description={r ? `${r.condition}${r.service ? ` · service ${r.service}` : ''}${r.route ? ` · route ${r.route}` : ''}${r.environment ? ` · env ${r.environment}` : ''}${r.log_query ? ` · logs ${r.log_query}` : ''}` : undefined}
        actions={
          r && (
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={stateTone[r.state]} className="text-xs" data-testid="alert-state" data-state={r.state}>
                {stateLabel[r.state]}
              </Badge>
              {muted ? (
                <Button variant="outline" size="sm" disabled={busy} onClick={() => act(() => api.muteAlert(d!.project, r.id, 0))}>
                  Unmute
                </Button>
              ) : (
                <>
                  <Button variant="outline" size="sm" disabled={busy} onClick={() => act(() => api.muteAlert(d!.project, r.id, 60))} data-testid="snooze-1h">
                    Snooze 1h
                  </Button>
                  <Button variant="outline" size="sm" disabled={busy} onClick={() => act(() => api.muteAlert(d!.project, r.id, 24 * 60))}>
                    Snooze 24h
                  </Button>
                </>
              )}
              <Link href={`/alerts/${r.id}/edit`} className="inline-flex h-7 items-center rounded-md border border-border px-2.5 text-xs hover:bg-accent">
                Edit
              </Link>
              <Button
                variant="danger"
                size="sm"
                disabled={busy}
                onClick={() => {
                  if (confirm(`Delete "${r.name}" and its history?`)) act(async () => (await api.deleteAlert(d!.project, r.id), router.push('/alerts')));
                }}
              >
                Delete
              </Button>
            </div>
          )
        }
      />
      <ErrorNote error={data.error ?? error} />
      {data.loading && !d && <Spinner />}
      {r && d && (
        <div className="grid gap-4 xl:grid-cols-[1fr_420px]">
          <div className="flex flex-col gap-4">
            <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-4">
              {[
                ['State since', ago(r.state_since)],
                ['Last value', value(r.last_value, r.unit)],
                ['Checked', r.last_evaluated_at ? ago(r.last_evaluated_at) : 'not yet'],
                ['Notifications', muted ? `muted until ${new Date(r.muted_until!).toLocaleTimeString()}` : r.renotify_seconds ? `repeat every ${span(r.renotify_seconds)}` : 'on change'],
              ].map(([k, v]) => (
                <div key={k} className="bg-surface px-4 py-3">
                  <div className="text-[11px] font-medium uppercase tracking-wider text-subtle">{k}</div>
                  <div className="mt-1 text-sm font-medium" data-testid={k === 'Notifications' ? 'alert-notifications' : undefined}>
                    {v}
                  </div>
                </div>
              ))}
            </div>
            <Card>
              <CardHeader title="Metric vs threshold" description={d.preview?.supported ? `Last ${d.preview.points.length} buckets of ${span(d.preview.bucket_seconds)}` : undefined} />
              <div className="p-3">{d.preview ? <AlertChart preview={d.preview} /> : <Empty>No chart.</Empty>}</div>
            </Card>
          </div>
          <Card className="h-fit">
            <CardHeader title="History" description="State changes and notifications, newest first." />
            {d.detail.history.length === 0 ? (
              <Empty>Nothing yet. {r.enabled ? `The rule is checked every ${span(r.interval_seconds)}.` : 'The rule is disabled.'}</Empty>
            ) : (
              <ol className="max-h-[640px] overflow-auto p-4" data-testid="alert-history">
                {d.detail.history.map((e) => (
                  <Entry key={e.id} e={e} unit={r.unit} />
                ))}
              </ol>
            )}
          </Card>
        </div>
      )}
    </>
  );
}
