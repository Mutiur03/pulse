'use client';
import Link from 'next/link';
import { api, currentProjectId } from '@/lib/api';
import { ago } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { stateLabel, stateTone, value } from '@/lib/alerts';
import { Badge, Card, Empty, ErrorNote, Loading, PageHeader, Spinner, Td, Th } from '@/components/ui';

export default function AlertsPage() {
  const rules = useAsync(async () => api.listAlerts(await currentProjectId()), []);
  const list = rules.data ?? [];
  const firing = list.filter((r) => r.state === 'alerting').length;
  return (
    <>
      <PageHeader
        title="Alerts"
        description={rules.data ? `${list.length} rules, ${firing} alerting` : 'Rules that watch errors, latency, traffic and events.'}
        actions={
          <div className="flex gap-2">
            <Link href="/alerts/channels" className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm hover:bg-accent">
              Channels
            </Link>
            <Link href="/alerts/new" className="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-fg hover:bg-primary/90" data-testid="new-alert">
              New rule
            </Link>
          </div>
        }
      />
      <ErrorNote error={rules.error} />
      {rules.loading && !rules.data && <Spinner />}
      <Card className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="border-b border-border bg-accent/40">
            <tr>
              <Th>State</Th>
              <Th>Rule</Th>
              <Th>Last value</Th>
              <Th>Since</Th>
              <Th>Checked</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {list.map((r) => (
              <tr key={r.id} className="hover:bg-accent/40" data-testid="alert-row" data-state={r.state} data-name={r.name}>
                <Td>
                  <Badge tone={stateTone[r.state]}>{stateLabel[r.state]}</Badge>
                </Td>
                <Td>
                  <Link href={`/alerts/${r.id}`} className="font-medium hover:text-primary">
                    {r.name}
                  </Link>
                  <div className="font-mono text-[11px] text-muted">
                    {r.condition}
                    {r.service && ` · ${r.service}`}
                    {r.route && ` · ${r.route}`}
                    {r.environment && ` · ${r.environment}`}
                    {r.log_query && ` · ${r.log_query}`}
                  </div>
                  <div className="mt-0.5 flex gap-1">
                    {!r.enabled && <Badge>disabled</Badge>}
                    {r.muted_until && Date.parse(r.muted_until) > Date.now() && <Badge tone="warn">muted until {new Date(r.muted_until).toLocaleString()}</Badge>}
                  </div>
                </Td>
                <Td className="font-mono text-xs">{value(r.last_value, r.unit)}</Td>
                <Td className="whitespace-nowrap text-xs text-muted">{ago(r.state_since)}</Td>
                <Td className="whitespace-nowrap text-xs text-muted">{r.last_evaluated_at ? ago(r.last_evaluated_at) : 'not yet'}</Td>
              </tr>
            ))}
          </tbody>
        </table>
        {rules.loading && !rules.data && <Loading rows={3} />}
        {rules.data && list.length === 0 && (
          <Empty title="No alert rules yet" action={{ label: 'Create an alert', href: '/alerts/new' }}>
            Get notified about new errors, slow routes or traffic drops.
          </Empty>
        )}
      </Card>
    </>
  );
}
