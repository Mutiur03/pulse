'use client';
import Link from 'next/link';
import type { DeletionRequest, RetentionSettings } from '@pulse/api-client';
import { time } from '@/lib/format';
import { useDataGovernance } from '@/lib/use-data-governance';
import { Badge, Button, Card, CardHeader, Empty, ErrorNote, Input, PageHeader, Spinner, Td, Th } from '@/components/ui';

const kinds: { key: keyof RetentionSettings; label: string }[] = [
  { key: 'events_days', label: 'Product events and sessions' },
  { key: 'traces_days', label: 'Traces' },
  { key: 'logs_days', label: 'Logs' },
  { key: 'errors_days', label: 'Errors' },
  { key: 'replays_days', label: 'Session replays' },
  { key: 'sourcemaps_days', label: 'Source maps' },
];

const tone = (s: DeletionRequest['status']) => (s === 'completed' ? 'ok' : s === 'failed' ? 'danger' : 'warn');

export default function DataSettingsPage() {
  const { me, project, owner, settings, requests, rules, setRules, keys, setKeys, patterns, setPatterns, retention, setRetention, sample, setSample, preview, person, setPerson, reason, setReason, busy, err, saved, save, runPreview, requestDeletion } = useDataGovernance();
  const limits = settings.data?.limits;
  return (
    <>
      <PageHeader
        title="Data governance"
        description="PII scrubbing at ingest, retention periods and deletion requests for this project."
        actions={
          <Link href="/settings" className="text-sm text-muted hover:text-fg">
            Back to settings
          </Link>
        }
      />
      <ErrorNote error={me.error ?? settings.error ?? requests.error ?? err} />
      {!owner && project && <p className="mb-4 text-sm text-muted">Only project owners can change these settings or request deletions.</p>}
      {(!rules || !retention || !limits) && !settings.error ? (
        <Spinner />
      ) : (
        rules &&
        retention &&
        limits && (
          <form onSubmit={save} className="grid gap-4 xl:grid-cols-2">
            <Card data-testid="scrub-settings">
              <CardHeader
                title="PII scrubbing"
                description="Applied by the ingest gateway before data is stored, to event properties, error messages and stacks, log bodies and attributes, span attributes and replay text. Changes apply within 30 seconds."
              />
              <div className="space-y-3 p-4 text-sm">
                {(
                  [
                    ['emails', 'Email addresses', '[email]'],
                    ['cards', 'Card numbers', '[card]'],
                    ['ips', 'IP addresses', '[ip]'],
                  ] as const
                ).map(([k, label, marker]) => (
                  <label key={k} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      data-testid={`scrub-${k}`}
                      checked={rules[k]}
                      disabled={!owner}
                      onChange={(e) => setRules({ ...rules, [k]: e.target.checked })}
                    />
                    {label} <span className="font-mono text-xs text-muted">→ {marker}</span>
                  </label>
                ))}
                <label className="block">
                  <span className="text-muted">Sensitive keys, one per line (values become [redacted])</span>
                  <textarea
                    data-testid="scrub-keys"
                    className="mt-1 h-24 w-full rounded-md border border-border bg-transparent p-2 font-mono text-xs"
                    value={keys}
                    disabled={!owner}
                    onChange={(e) => setKeys(e.target.value)}
                  />
                </label>
                <label className="block">
                  <span className="text-muted">Custom patterns, one regular expression per line</span>
                  <textarea
                    data-testid="scrub-patterns"
                    className="mt-1 h-16 w-full rounded-md border border-border bg-transparent p-2 font-mono text-xs"
                    value={patterns}
                    disabled={!owner}
                    onChange={(e) => setPatterns(e.target.value)}
                  />
                </label>
                <label className="block">
                  <span className="text-muted">Try it on a sample (text or JSON)</span>
                  <textarea
                    data-testid="scrub-sample"
                    className="mt-1 h-16 w-full rounded-md border border-border bg-transparent p-2 font-mono text-xs"
                    value={sample}
                    onChange={(e) => setSample(e.target.value)}
                  />
                </label>
                <Button type="button" size="sm" variant="outline" onClick={runPreview} disabled={busy} data-testid="scrub-preview">
                  Preview
                </Button>
                {preview !== undefined && (
                  <pre className="overflow-auto rounded-md bg-raised p-2 font-mono text-xs" data-testid="scrub-result">
                    {preview}
                  </pre>
                )}
              </div>
            </Card>
            <Card data-testid="retention-settings">
              <CardHeader
                title="Retention"
                description="Days to keep each kind of data. Periods can be shortened below the platform limit; the governance worker deletes older data every hour. Source maps: 0 keeps them until deleted."
              />
              <div className="space-y-2 p-4 text-sm">
                {kinds.map(({ key, label }) => (
                  <label key={key} className="flex items-center justify-between gap-3">
                    <span>{label}</span>
                    <span className="flex items-center gap-2">
                      <Input
                        type="number"
                        data-testid={`retention-${key}`}
                        min={key === 'sourcemaps_days' ? 0 : 1}
                        max={limits[key]}
                        value={retention[key]}
                        disabled={!owner}
                        onChange={(e) => setRetention({ ...retention, [key]: Number(e.target.value) })}
                        className="w-24"
                      />
                      <span className="w-20 text-xs text-muted">max {limits[key]}</span>
                    </span>
                  </label>
                ))}
              </div>
              {owner && (
                <div className="flex items-center gap-3 border-t border-border p-4">
                  <Button type="submit" size="sm" disabled={busy} data-testid="save-data-settings">
                    Save settings
                  </Button>
                  {saved && <span className="text-sm text-muted">Saved</span>}
                  {settings.data?.updated_at && <span className="text-xs text-muted">Last changed {time(settings.data.updated_at)}</span>}
                </div>
              )}
            </Card>
          </form>
        )
      )}
      <Card className="mt-4" data-testid="deletion-requests">
        <CardHeader
          title="Deletion requests"
          description="Deletes every event, session, replay, error, trace and log of a person and all ids linked to them. Each request is recorded in the audit log."
        />
        {owner && (
          <form onSubmit={requestDeletion} className="flex flex-wrap gap-2 border-b border-border p-4">
            <Input aria-label="Person id" placeholder="Distinct id or person id" value={person} onChange={(e) => setPerson(e.target.value)} className="max-w-xs" data-testid="deletion-id" />
            <Input aria-label="Reason" placeholder="Reason (optional)" value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} className="max-w-sm" />
            <Button type="submit" size="sm" variant="outline" disabled={busy || !person.trim()} data-testid="request-deletion">
              Request deletion
            </Button>
          </form>
        )}
        {requests.data?.length === 0 ? (
          <Empty>No deletion requests yet.</Empty>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr>
                <Th>Requested</Th>
                <Th>Person</Th>
                <Th>Ids</Th>
                <Th>Status</Th>
                <Th>Deleted</Th>
              </tr>
            </thead>
            <tbody>
              {requests.data?.map((r) => (
                <tr key={r.id} className="border-t border-border" data-testid="deletion-row">
                  <Td className="whitespace-nowrap">{time(r.created_at)}</Td>
                  <Td className="font-mono text-xs">
                    {r.distinct_id}
                    {r.reason && <div className="font-sans text-muted">{r.reason}</div>}
                  </Td>
                  <Td className="font-mono text-xs">{r.distinct_ids.join(', ')}</Td>
                  <Td>
                    <Badge tone={tone(r.status)} data-testid="deletion-status">
                      {r.status}
                    </Badge>
                    {r.error && <div className="text-xs text-danger-fg">{r.error}</div>}
                  </Td>
                  <Td className="text-xs text-muted">
                    {Object.entries(r.result)
                      .filter(([, n]) => n > 0)
                      .map(([k, n]) => `${k} ${n}`)
                      .join(' · ') || (r.status === 'completed' ? 'nothing found' : '')}
                    {r.completed_at && <div>{time(r.completed_at)}</div>}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  );
}
