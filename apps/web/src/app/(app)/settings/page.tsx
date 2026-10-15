'use client';
import { useState } from 'react';
import { api, handleAuthError } from '@/lib/api';
import { time } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { Badge, Button, Card, CardHeader, ErrorNote, PageHeader, Spinner } from '@/components/ui';

export default function SettingsPage() {
  const me = useAsync(() => api.me(), []);
  const project = me.data?.projects[0];
  const keys = useAsync(() => (project ? api.listKeys(project.id) : Promise.resolve([])), [project?.id]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string>();
  const [copied, setCopied] = useState(false);
  const active = keys.data?.find((k) => !k.revoked_at);
  const ingest = typeof window !== 'undefined' ? `${location.protocol}//${location.hostname}:8080` : 'http://localhost:8080';

  const rotate = async () => {
    if (!project || !confirm('Rotate the ingest key? The current key stops working immediately, so update your apps first.')) return;
    setBusy(true);
    try {
      await api.rotateKey(project.id);
      keys.reload();
    } catch (e) {
      handleAuthError(e);
      setErr(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };
  const snippet = `import { init, capture, identify } from '@pulse/browser';

init({
  endpoint: '${ingest}',
  apiKey: '${active?.public_key ?? 'pk_…'}',
  serviceName: 'web',
  // Send traceparent/baggage to your API so its spans join browser traces.
  propagateTraceHeaderCorsUrls: [/^https:\\/\\/api\\.example\\.com/],
});

identify('user_123', { plan: 'pro' });
capture('checkout_started', { items: 3 });`;
  const backend = `OTEL_EXPORTER_OTLP_ENDPOINT=${ingest}
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS=X-Pulse-Key=${active?.public_key ?? 'pk_…'}
OTEL_SERVICE_NAME=checkout-api`;

  return (
    <>
      <PageHeader title="Settings" description={project ? `Project: ${project.name}` : undefined} />
      <ErrorNote error={me.error ?? keys.error ?? err} />
      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader
            title="Ingest keys"
            description="Public keys identify the project when sending data. They are safe to ship in browser code."
            actions={
              project?.role === 'owner' && (
                <Button size="sm" variant="outline" onClick={rotate} disabled={busy} data-testid="rotate-key">
                  Rotate key
                </Button>
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
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      void navigator.clipboard?.writeText(k.public_key);
                      setCopied(true);
                      setTimeout(() => setCopied(false), 1500);
                    }}
                  >
                    {copied ? 'Copied' : 'Copy'}
                  </Button>
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
    </>
  );
}
