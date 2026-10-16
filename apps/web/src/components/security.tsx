'use client';
import { useEffect, useState } from 'react';
import type { AuditEntry, Org } from '@pulse/api-client';
import { api, handleAuthError } from '@/lib/api';
import { time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, ErrorNote, Spinner } from '@/components/ui';

/** Browser origins allowed to send data with this project's ingest keys. */
export function AllowedOrigins({ project, owner }: { project: number; owner: boolean }) {
  const current = useAsync(() => api.getAllowedOrigins(project), [project]);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (current.data) setText(current.data.origins.join('\n'));
  }, [current.data]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(undefined);
    setSaved(false);
    try {
      const origins = text
        .split(/[\s,]+/)
        .map((o) => o.trim())
        .filter(Boolean);
      const out = await api.setAllowedOrigins(project, origins);
      setText(out.origins.join('\n'));
      setSaved(true);
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };

  return (
    <Card className="mt-4">
      <CardHeader
        title="Allowed origins"
        description="Browser pages allowed to send data with this project's keys, one per line, e.g. https://shop.example.com or https://*.example.com. Leave empty to accept any origin. Servers that send no Origin header are not affected."
      />
      <ErrorNote error={current.error ?? err} />
      <form onSubmit={save} className="space-y-2 p-4">
        <textarea
          aria-label="Allowed origins"
          className="h-24 w-full rounded-md border border-border bg-zinc-950 p-2 font-mono text-xs text-zinc-200 disabled:opacity-60"
          placeholder="Any origin"
          value={text}
          disabled={!owner || busy}
          onChange={(e) => {
            setText(e.target.value);
            setSaved(false);
          }}
          data-testid="allowed-origins"
        />
        {owner ? (
          <div className="flex items-center gap-2">
            <Button type="submit" size="sm" disabled={busy}>
              Save origins
            </Button>
            {saved && (
              <span className="text-xs text-emerald-400" data-testid="origins-saved">
                Saved. Ingest applies it within seconds.
              </span>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted">Only project owners can change this.</p>
        )}
      </form>
    </Card>
  );
}

/** Sign out of every browser and device. */
export function Sessions() {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const logoutAll = async () => {
    if (!confirm('Sign out of Pulse everywhere, including this browser?')) return;
    setBusy(true);
    try {
      await api.logoutAll();
      window.location.href = '/login';
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  };
  return (
    <Card className="mt-4">
      <CardHeader
        title="Sessions"
        description="Sessions end after 7 days, or after a day without use. Sign out everywhere if a device was lost or shared."
        actions={
          <Button size="sm" variant="outline" onClick={() => void logoutAll()} disabled={busy} data-testid="logout-all" className="whitespace-nowrap">
            Sign out everywhere
          </Button>
        }
      />
      <ErrorNote error={err} />
    </Card>
  );
}

const actionTone = (a: string) => (a.includes('failure') || a.includes('blocked') || a.includes('revoke') || a.includes('remove') ? 'danger' : 'neutral');

/** Security audit log of the organization (owners only). */
export function AuditLog({ org }: { org: Org }) {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [more, setMore] = useState(true);
  const first = useAsync(() => api.listAuditLog(org.id, { limit: 25 }), [org.id]);
  useEffect(() => {
    if (first.data) {
      setEntries(first.data);
      setMore(first.data.length === 25);
    }
  }, [first.data]);
  const loadMore = async () => {
    const last = entries[entries.length - 1];
    if (!last) return;
    const next = await api.listAuditLog(org.id, { limit: 25, before: last.id });
    setEntries([...entries, ...next]);
    setMore(next.length === 25);
  };

  return (
    <Card className="mt-4">
      <CardHeader title="Audit log" description={`Security-relevant actions in ${org.name}: logins, keys, tokens, invites and roles.`} />
      <ErrorNote error={first.error} />
      {first.loading && (
        <div className="p-4">
          <Spinner />
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-xs" data-testid="audit-log">
          <thead className="text-muted">
            <tr className="border-b border-border">
              <th className="px-4 py-2 font-medium">When</th>
              <th className="px-4 py-2 font-medium">Action</th>
              <th className="px-4 py-2 font-medium">Who</th>
              <th className="px-4 py-2 font-medium">Target</th>
              <th className="px-4 py-2 font-medium">Address</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {entries.map((e) => (
              <tr key={e.id} data-testid="audit-row">
                <td className="whitespace-nowrap px-4 py-2 text-muted">{time(e.created_at)}</td>
                <td className="px-4 py-2">
                  <Badge tone={actionTone(e.action)}>{e.action}</Badge>
                </td>
                <td className="px-4 py-2 text-zinc-300">{e.actor_email || '-'}</td>
                <td className="max-w-xs truncate px-4 py-2 text-zinc-300">{e.target || '-'}</td>
                <td className="px-4 py-2 font-mono text-muted">{e.ip || '-'}</td>
              </tr>
            ))}
            {!first.loading && entries.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-3 text-muted">
                  Nothing recorded yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {more && entries.length > 0 && (
        <div className="border-t border-border p-3">
          <Button size="sm" variant="ghost" onClick={() => void loadMore()}>
            Older entries
          </Button>
        </div>
      )}
    </Card>
  );
}
