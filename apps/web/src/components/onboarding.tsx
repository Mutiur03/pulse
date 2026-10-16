'use client';
import Link from 'next/link';
import { useEffect, useState } from 'react';
import { api, handleAuthError } from '@/lib/api';
import { cn } from '@/lib/cn';
import { backendPlatforms, installCommand, platformLabels, platformSnippet, type BackendPlatform } from '@/lib/snippet';
import { Button, Card, CardHeader, Spinner } from '@/components/ui';

type Platform = 'browser' | BackendPlatform;

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      size="sm"
      variant="ghost"
      onClick={() => {
        void navigator.clipboard?.writeText(text);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }}
    >
      {copied ? 'Copied' : 'Copy'}
    </Button>
  );
}

/** Step 2 of onboarding: install instructions per platform, with the key filled in. */
function InstallGuide({ apiKey }: { apiKey: string }) {
  const [platform, setPlatform] = useState<Platform>('browser');
  const install = installCommand(platform);
  const snippet = platformSnippet(platform, apiKey);
  return (
    <Card>
      <CardHeader title="2. Install an SDK" description="Pick where your code runs. Browser events and backend spans of the same request join into one trace." />
      <div className="flex flex-wrap gap-1 border-b border-border px-4 py-2" role="tablist">
        {(['browser', ...backendPlatforms] as Platform[]).map((p) => (
          <button
            key={p}
            type="button"
            role="tab"
            aria-selected={platform === p}
            data-testid={`platform-${p}`}
            onClick={() => setPlatform(p)}
            className={cn('rounded-md px-3 py-1.5 text-xs', platform === p ? 'bg-accent text-fg' : 'text-muted hover:text-fg')}
          >
            {platformLabels[p]}
          </button>
        ))}
      </div>
      {install && (
        <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3">
          <pre className="overflow-auto font-mono text-xs text-fg-2">{install}</pre>
          <CopyButton text={install} />
        </div>
      )}
      <div className="flex items-start justify-between gap-3 px-4 py-3">
        <pre className="overflow-auto font-mono text-xs leading-relaxed text-fg-2" data-testid={platform === 'browser' ? 'new-project-snippet' : 'platform-snippet'}>
          {snippet}
        </pre>
        <CopyButton text={snippet} />
      </div>
    </Card>
  );
}

interface FirstData {
  kind: 'event' | 'trace';
  name: string;
  at: string;
}

/**
 * Step 3 of onboarding: polls the project for its first event or trace and
 * says so when one arrives.
 */
function FirstEventCheck({ project }: { project: number }) {
  const [first, setFirst] = useState<FirstData>();
  const [checks, setChecks] = useState(0);
  useEffect(() => {
    if (first) return;
    let stopped = false;
    const from = new Date(Date.now() - 30 * 86400e3).toISOString();
    const check = async () => {
      try {
        const [ev, tr] = await Promise.all([api.listEvents({ project_id: project, from, limit: 1 }), api.listTraces({ project_id: project, from, limit: 1 })]);
        if (stopped) return;
        const e = ev.events[0];
        const t = tr.traces[0];
        if (e) setFirst({ kind: 'event', name: e.event, at: e.timestamp });
        else if (t) setFirst({ kind: 'trace', name: t.root_name || t.trace_id, at: t.start_time });
        else setChecks((n) => n + 1);
      } catch (err) {
        handleAuthError(err);
      }
    };
    void check();
    const id = setInterval(check, 3000);
    return () => {
      stopped = true;
      clearInterval(id);
    };
  }, [project, first]);

  return (
    <Card data-testid="first-event">
      <CardHeader title="3. Verify" description="Run your app and do something: load a page, call an endpoint." />
      <div className="px-4 py-4 text-sm">
        {first ? (
          <div className="flex flex-col gap-3" data-testid="first-event-received">
            <div className="flex items-center gap-2 text-ok">
              <span className="h-2 w-2 rounded-full bg-ok" />
              First {first.kind} received: <span className="font-mono text-fg">{first.name}</span>
            </div>
            <div className="flex gap-2">
              <Link href={first.kind === 'event' ? '/events' : '/traces'} className="inline-flex h-8 items-center rounded-md bg-primary px-3 text-xs font-medium text-primary-fg">
                Open {first.kind === 'event' ? 'events' : 'traces'}
              </Link>
              <Link href="/dashboards" className="inline-flex h-8 items-center rounded-md px-3 text-xs text-fg-2 hover:bg-accent">
                Go to dashboards
              </Link>
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-3 text-muted" data-testid="first-event-waiting">
            <Spinner />
            Waiting for the first event or trace{checks > 20 ? '. Nothing yet: check the endpoint, the key and the browser console.' : '…'}
          </div>
        )}
      </div>
    </Card>
  );
}

/** Step 1 of onboarding: the project's ingest key. */
function KeyCard({ apiKey }: { apiKey: string }) {
  return (
    <Card>
      <CardHeader title="1. Your ingest key" description="Public: safe to ship in browser code. Add, rotate or revoke keys in Settings." />
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <code className="truncate font-mono text-sm" data-testid="new-project-key">
          {apiKey}
        </code>
        <CopyButton text={apiKey} />
      </div>
    </Card>
  );
}

/** The whole setup flow for a project: key, install, verify. */
export function SetupSteps({ name, project, apiKey }: { name: string; project: number; apiKey: string }) {
  return (
    <>
      <div className="mb-6">
        <h1 className="text-xl font-semibold">Set up {name}</h1>
        <p className="mt-1 text-sm text-muted">Three steps: copy the key, add an SDK, and check that data arrives.</p>
      </div>
      <div className="flex flex-col gap-4">
        <KeyCard apiKey={apiKey} />
        <InstallGuide apiKey={apiKey} />
        <FirstEventCheck project={project} />
      </div>
    </>
  );
}
