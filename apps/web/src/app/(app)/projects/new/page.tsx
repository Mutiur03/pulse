'use client';
import { useState } from 'react';
import { api, currentContext, handleAuthError, selectProject } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { Button, Card, ErrorNote, Input, PageHeader } from '@/components/ui';

export default function NewProjectPage() {
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const me = useAsync(() => api.me(), []);
  const org = me.data ? currentContext(me.data).org : undefined;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(undefined);
    try {
      const res = await api.createProject(name.trim(), org?.id);
      selectProject(res.project.id);
      // A full load refreshes the project switcher; /setup shows the key,
      // install steps and the first event check.
      window.location.assign('/setup');
      return;
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };

  return (
    <div className="soft-auth">
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
    </div>
  );
}
