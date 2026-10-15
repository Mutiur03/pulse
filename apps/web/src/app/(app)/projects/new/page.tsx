'use client';
import Link from 'next/link';
import { useState } from 'react';
import type { CreatedProject } from '@pulse/api-client';
import { api, currentContext, handleAuthError, selectProject } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { backendSnippet, browserSnippet } from '@/lib/snippet';
import { Button, Card, CardHeader, ErrorNote, Input, PageHeader } from '@/components/ui';

export default function NewProjectPage() {
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const [created, setCreated] = useState<CreatedProject>();
  const [copied, setCopied] = useState(false);
  const me = useAsync(() => api.me(), []);
  const org = me.data ? currentContext(me.data).org : undefined;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(undefined);
    try {
      const res = await api.createProject(name.trim(), org?.id);
      selectProject(res.project.id);
      setCreated(res);
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };

  if (created) {
    const key = created.key.public_key;
    return (
      <>
        <PageHeader title={`${created.project.name} is ready`} description="Send data with this ingest key. You can add, rotate or revoke keys in Settings." />
        <div className="grid gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader title="Ingest key" description="Public: safe to ship in browser code." />
            <div className="flex items-center justify-between gap-3 px-4 py-3">
              <code className="truncate font-mono text-sm" data-testid="new-project-key">
                {key}
              </code>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  void navigator.clipboard?.writeText(key);
                  setCopied(true);
                }}
              >
                {copied ? 'Copied' : 'Copy'}
              </Button>
            </div>
            <div className="border-t border-border px-4 py-3 text-xs text-muted">Backend services (any OpenTelemetry SDK):</div>
            <pre className="overflow-auto px-4 pb-4 font-mono text-xs leading-relaxed text-zinc-300">{backendSnippet(key)}</pre>
          </Card>
          <Card>
            <CardHeader title="Browser SDK" description="Install @pulse/browser and initialise it once on page load." />
            <pre className="overflow-auto p-4 font-mono text-xs leading-relaxed text-zinc-300" data-testid="new-project-snippet">
              {browserSnippet(key)}
            </pre>
          </Card>
        </div>
        <div className="mt-4 flex gap-2">
          <Button onClick={() => (window.location.href = '/traces')}>Go to traces</Button>
          <Link href="/settings" className="inline-flex h-9 items-center rounded-md px-3 text-sm text-zinc-300 hover:bg-accent">
            Project settings
          </Link>
        </div>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="New project"
        description={`A project holds one app's traces, errors and events, with its own ingest keys.${org ? ` It is created in ${org.name}.` : ''}`}
      />
      <Card className="max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-3 p-4">
          <label className="flex flex-col gap-1 text-xs text-muted">
            Project name
            <Input name="project-name" required maxLength={80} placeholder="e.g. Web shop" value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <ErrorNote error={err} />
          <Button type="submit" disabled={busy || !name.trim()}>
            {busy ? 'Creating…' : 'Create project'}
          </Button>
        </form>
      </Card>
    </>
  );
}
