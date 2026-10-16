'use client';
import Link from 'next/link';
import { useState } from 'react';
import type { AlertChannel, AlertChannelKind } from '@pulse/api-client';
import { api, currentProjectId } from '@/lib/api';
import { ago } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, Empty, ErrorNote, Input, PageHeader, Select, Spinner, Td, Th } from '@/components/ui';

const placeholders: Record<AlertChannelKind, string> = {
  email: 'oncall@example.com',
  webhook: 'https://example.com/pulse-webhook',
  slack: 'https://hooks.slack.com/services/…',
};

export default function ChannelsPage() {
  const data = useAsync(async () => {
    const project = await currentProjectId();
    return { project, channels: await api.listAlertChannels(project) };
  }, []);
  const [name, setName] = useState('');
  const [kind, setKind] = useState<AlertChannelKind>('email');
  const [target, setTarget] = useState('');
  const [error, setError] = useState<string>();
  const [created, setCreated] = useState<AlertChannel>();
  const [tests, setTests] = useState<Record<number, string>>({});

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(undefined);
    try {
      const c = await api.createAlertChannel(data.data!.project, { name, kind, target });
      setCreated(c);
      setName('');
      setTarget('');
      data.reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };
  const test = async (c: AlertChannel) => {
    setTests((t) => ({ ...t, [c.id]: 'Sending…' }));
    const res = await api.testAlertChannel(data.data!.project, c.id).catch((e: Error) => ({ ok: false, error: e.message }));
    setTests((t) => ({ ...t, [c.id]: res.ok ? 'Sent' : `Failed: ${res.error}` }));
  };
  const remove = async (c: AlertChannel) => {
    if (!confirm(`Delete channel "${c.name}"? Rules stop sending to it.`)) return;
    await api.deleteAlertChannel(data.data!.project, c.id).catch((e: Error) => setError(e.message));
    data.reload();
  };

  return (
    <>
      <div className="mb-2 text-xs text-muted">
        <Link href="/alerts" className="hover:text-fg">
          Alerts
        </Link>{' '}
        / Channels
      </div>
      <PageHeader title="Notification channels" description="Where alert rules send their notifications. Each rule picks its channels." />
      <div className="grid gap-4 xl:grid-cols-[1fr_380px]">
        <Card className="h-fit overflow-x-auto">
          {data.loading && !data.data && (
            <div className="p-4">
              <Spinner />
            </div>
          )}
          <table className="w-full text-sm">
            <thead className="border-b border-border bg-accent/40">
              <tr>
                <Th>Channel</Th>
                <Th>Target</Th>
                <Th>Added</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {data.data?.channels.map((c) => (
                <tr key={c.id} data-testid="channel-row" data-name={c.name}>
                  <Td>
                    <div className="font-medium">{c.name}</div>
                    <Badge tone={c.kind === 'email' ? 'browser' : c.kind === 'slack' ? 'backend' : 'database'}>{c.kind}</Badge>
                  </Td>
                  <Td className="max-w-[280px] truncate font-mono text-xs">{c.target}</Td>
                  <Td className="whitespace-nowrap text-xs text-muted">{ago(c.created_at)}</Td>
                  <Td className="whitespace-nowrap text-right">
                    {tests[c.id] && (
                      <span className={`mr-2 text-xs ${tests[c.id].startsWith('Failed') ? 'text-danger-fg' : 'text-muted'}`} data-testid="channel-test-result">
                        {tests[c.id]}
                      </span>
                    )}
                    <Button variant="outline" size="sm" onClick={() => test(c)} data-testid="channel-test">
                      Send test
                    </Button>{' '}
                    <Button variant="ghost" size="sm" onClick={() => remove(c)}>
                      Delete
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
          {data.data?.channels.length === 0 && <Empty>No channels yet.</Empty>}
        </Card>
        <Card className="h-fit">
          <CardHeader title="Add a channel" />
          <form onSubmit={add} className="flex flex-col gap-3 p-4">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name, e.g. On-call email" aria-label="Channel name" required />
            <Select value={kind} onChange={(e) => setKind(e.target.value as AlertChannelKind)} aria-label="Kind">
              <option value="email">Email</option>
              <option value="webhook">Webhook (signed)</option>
              <option value="slack">Slack incoming webhook</option>
            </Select>
            <Input value={target} onChange={(e) => setTarget(e.target.value)} placeholder={placeholders[kind]} aria-label="Target" required />
            <ErrorNote error={error ?? data.error} />
            <Button type="submit" disabled={!data.data}>
              Add channel
            </Button>
            {kind === 'webhook' && (
              <p className="text-xs text-muted">
                Requests carry <code>X-Pulse-Signature: t=&lt;unix&gt;,v1=&lt;hex&gt;</code>, an HMAC-SHA256 of <code>&lt;unix&gt;.&lt;body&gt;</code> with the channel secret.
              </p>
            )}
          </form>
          {created?.secret && (
            <div className="border-t border-border p-4 text-xs" data-testid="channel-secret">
              <div className="mb-1 text-fg-2">Signing secret for {created.name}. Copy it now; it is not shown again.</div>
              <code className="block break-all rounded bg-background p-2 font-mono">{created.secret}</code>
            </div>
          )}
        </Card>
      </div>
    </>
  );
}
