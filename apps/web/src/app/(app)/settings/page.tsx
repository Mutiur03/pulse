'use client';
import { useState } from 'react';
import { api, currentContext, handleAuthError } from '@/lib/api';
import { Members } from '@/components/members';
import { UploadTokens } from '@/components/upload-tokens';
import { AllowedOrigins, AuditLog, Sessions } from '@/components/security';
import { backendSnippet, browserSnippet } from '@/lib/snippet';
import { time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, ErrorNote, Input, PageHeader, Spinner } from '@/components/ui';

export default function SettingsPage() {
  const me = useAsync(() => api.me(), []);
  const ctx = me.data ? currentContext(me.data) : undefined;
  const project = ctx?.project;
  const owner = project?.role === 'owner';
  const [name, setName] = useState('');
  const [copiedId, setCopiedId] = useState<number>();
  const keys = useAsync(() => (project ? api.listKeys(project.id) : Promise.resolve([])), [project?.id]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const active = keys.data?.find((k) => !k.revoked_at);

  const act = async (fn: () => Promise<unknown>, after: () => void = keys.reload) => {
    setBusy(true);
    setErr(undefined);
    try {
      await fn();
      after();
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };
  const rotate = () => {
    if (!project || !confirm('Rotate the ingest key? Every current key stops working immediately, so update your apps first.')) return;
    void act(() => api.rotateKey(project.id));
  };
  const addKey = () => project && void act(() => api.createKey(project.id));
  const revoke = (id: number, key: string) => {
    if (!project || !confirm(`Revoke ${key}? Apps using it stop sending data immediately.`)) return;
    void act(() => api.revokeKey(project.id, id));
  };
  const rename = (e: React.FormEvent) => {
    e.preventDefault();
    if (!project || !name.trim()) return;
    void act(() => api.renameProject(project.id, name.trim()), () => window.location.reload());
  };
  const snippet = browserSnippet(active?.public_key);
  const backend = backendSnippet(active?.public_key);

  return (
    <>
      <PageHeader title="Settings" description={project ? `Project: ${project.name}` : undefined} />
      <ErrorNote error={me.error ?? keys.error ?? err} />
      {owner && (
        <Card className="mb-4">
          <CardHeader title="Project" description="Only owners can rename the project." />
          <form onSubmit={rename} className="flex gap-2 p-4">
            <Input aria-label="Project name" placeholder={project?.name} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} className="max-w-xs" />
            <Button type="submit" size="sm" disabled={busy || !name.trim()}>
              Rename
            </Button>
          </form>
        </Card>
      )}
      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader
            title="Ingest keys"
            description="Public keys identify the project when sending data. They are safe to ship in browser code."
            actions={
              owner && (
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={addKey} disabled={busy} data-testid="add-key" className="whitespace-nowrap">
                    Add key
                  </Button>
                  <Button size="sm" variant="outline" onClick={rotate} disabled={busy} data-testid="rotate-key" className="whitespace-nowrap">
                    Rotate all
                  </Button>
                </div>
              )
            }
          />
          <div className="divide-y divide-border">
            {keys.loading && (
              <div className="p-4">
                <Spinner />
              </div>
            )}
            {keys.data?.map((k) => (
              <div key={k.id} className="flex items-center justify-between gap-3 px-4 py-3 text-sm">
                <div className="min-w-0">
                  <div className={`truncate font-mono ${k.revoked_at ? 'text-muted line-through' : ''}`} data-testid={k.revoked_at ? undefined : 'active-key'}>
                    {k.public_key}
                  </div>
                  <div className="text-xs text-muted">
                    Created {time(k.created_at)} · {k.rate_limit_per_sec} req/s
                    {k.revoked_at && ` · revoked ${time(k.revoked_at)}`}
                  </div>
                </div>
                {k.revoked_at ? (
                  <Badge>revoked</Badge>
                ) : (
                  <div className="flex shrink-0 gap-1">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        void navigator.clipboard?.writeText(k.public_key);
                        setCopiedId(k.id);
                        setTimeout(() => setCopiedId(undefined), 1500);
                      }}
                    >
                      {copiedId === k.id ? 'Copied' : 'Copy'}
                    </Button>
                    {owner && (
                      <Button size="sm" variant="ghost" onClick={() => revoke(k.id, k.public_key)} disabled={busy} data-testid="revoke-key">
                        Revoke
                      </Button>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </Card>
        <Card>
          <CardHeader title="Browser SDK" description="Install @pulse/browser and initialise it once on page load." />
          <pre className="overflow-auto p-4 font-mono text-xs leading-relaxed text-zinc-300" data-testid="sdk-snippet">
            {snippet}
          </pre>
          <div className="border-t border-border px-4 py-3 text-xs text-muted">Backend services (any OpenTelemetry SDK):</div>
          <pre className="overflow-auto px-4 pb-4 font-mono text-xs leading-relaxed text-zinc-300">{backend}</pre>
        </Card>
      </div>
      {project && <AllowedOrigins project={project.id} owner={owner} />}
      {project && <UploadTokens project={project.id} owner={owner} />}
      {ctx?.org && me.data && <Members org={ctx.org} me={me.data.user.id} />}
      {ctx?.org?.role === 'owner' && <AuditLog org={ctx.org} />}
      <Sessions />
    </>
  );
}
