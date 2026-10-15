'use client';
import { useState } from 'react';
import type { UploadTokenCreated } from '@pulse/api-client';
import { api, handleAuthError } from '@/lib/api';
import { time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { uploadSnippet } from '@/lib/snippet';
import { Badge, Button, Card, CardHeader, ErrorNote, Input, Spinner } from '@/components/ui';

/** Source map upload tokens: secret, project-scoped, separate from ingest keys. */
export function UploadTokens({ project, owner }: { project: number; owner: boolean }) {
  const tokens = useAsync(() => api.listUploadTokens(project), [project]);
  const [name, setName] = useState('');
  const [created, setCreated] = useState<UploadTokenCreated>();
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(undefined);
    try {
      await fn();
      tokens.reload();
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };
  const create = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    void act(async () => {
      setCreated(await api.createUploadToken(project, name.trim()));
      setName('');
    });
  };
  const revoke = (id: number, label: string) => {
    if (!confirm(`Revoke "${label}"? Builds using it can no longer upload source maps.`)) return;
    void act(() => api.revokeUploadToken(project, id));
  };

  return (
    <Card className="mt-4" data-testid="upload-tokens">
      <CardHeader
        title="Source map upload tokens"
        description="Secret tokens for pulse-cli or @pulse/vite-plugin to upload source maps. Unlike ingest keys, keep them out of browser code."
      />
      <ErrorNote error={tokens.error ?? err} />
      {owner && (
        <form onSubmit={create} className="flex gap-2 border-b border-border p-4">
          <Input aria-label="Token name" placeholder="e.g. CI deploys" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} className="max-w-xs" />
          <Button type="submit" size="sm" disabled={busy || !name.trim()} data-testid="create-upload-token">
            Create token
          </Button>
        </form>
      )}
      {created && (
        <div className="border-b border-border bg-accent/40 p-4 text-sm" data-testid="new-upload-token">
          <div className="mb-2 text-xs text-muted">Copy this token now. It is not shown again.</div>
          <div className="flex items-center gap-2">
            <code className="truncate font-mono text-xs" data-testid="upload-token-secret">
              {created.secret}
            </code>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                void navigator.clipboard?.writeText(created.secret);
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              }}
            >
              {copied ? 'Copied' : 'Copy'}
            </Button>
          </div>
          <pre className="mt-3 overflow-auto font-mono text-xs leading-relaxed text-zinc-300">{uploadSnippet(project, created.secret)}</pre>
        </div>
      )}
      <div className="divide-y divide-border">
        {tokens.loading && (
          <div className="p-4">
            <Spinner />
          </div>
        )}
        {tokens.data?.length === 0 && <div className="p-4 text-sm text-muted">No upload tokens yet.</div>}
        {tokens.data?.map((t) => (
          <div key={t.id} className="flex items-center justify-between gap-3 px-4 py-3 text-sm" data-testid="upload-token">
            <div className="min-w-0">
              <div className={t.revoked_at ? 'text-muted line-through' : ''}>
                {t.name} <span className="font-mono text-xs text-muted">pul_…{t.hint}</span>
              </div>
              <div className="text-xs text-muted">
                Created {time(t.created_at)} · {t.last_used_at ? `last used ${time(t.last_used_at)}` : 'never used'}
                {t.revoked_at && ` · revoked ${time(t.revoked_at)}`}
              </div>
            </div>
            {t.revoked_at ? (
              <Badge>revoked</Badge>
            ) : (
              owner && (
                <Button size="sm" variant="ghost" onClick={() => revoke(t.id, t.name)} disabled={busy} data-testid="revoke-upload-token">
                  Revoke
                </Button>
              )
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}
