'use client';
import Link from 'next/link';
import { useEffect, useState } from 'react';
import type { AlertChannel, AlertPreview, AlertRule, AlertRuleInput } from '@pulse/api-client';
import { api } from '@/lib/api';
import { comparisons, eventDriven, intervals, metrics, span, value, windows } from '@/lib/alerts';
import { AlertChart } from './alert-chart';
import { Button, Card, CardHeader, ErrorNote, Input, Select, Spinner } from './ui';

const blank: AlertRuleInput = {
  name: '',
  metric: 'error_rate',
  comparison: 'gt',
  threshold: 5,
  window_seconds: 300,
  interval_seconds: 60,
  service: '',
  route: '',
  environment: '',
  event_name: '',
  log_query: '',
  renotify_seconds: 0,
  channel_ids: [],
  enabled: true,
};

export function toInput(r: AlertRule): AlertRuleInput {
  const { name, metric, comparison, threshold, window_seconds, interval_seconds, service, route, environment, event_name, log_query, renotify_seconds, channel_ids, enabled } = r;
  return { name, metric, comparison, threshold, window_seconds, interval_seconds, service, route, environment, event_name, log_query, renotify_seconds, channel_ids, enabled };
}

function Field({ label, children, help }: { label: string; children: React.ReactNode; help?: string }) {
  return (
    <label className="flex flex-col gap-1 text-xs text-fg-2">
      {label}
      {children}
      {help && <span className="text-[11px] text-muted">{help}</span>}
    </label>
  );
}

export function AlertForm({ project, initial, onSave }: { project: number; initial?: Partial<AlertRuleInput>; onSave: (r: AlertRuleInput) => Promise<void> }) {
  const [r, setR] = useState<AlertRuleInput>({ ...blank, ...initial });
  const [channels, setChannels] = useState<AlertChannel[]>();
  const [preview, setPreview] = useState<AlertPreview>();
  const [previewing, setPreviewing] = useState(false);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const set = (p: Partial<AlertRuleInput>) => setR((x) => ({ ...x, ...p }));
  const isEvent = eventDriven(r.metric);
  const help = metrics.find((m) => m.value === r.metric)?.help;

  useEffect(() => {
    api.listAlertChannels(project).then(setChannels, () => setChannels([]));
  }, [project]);

  // Chart what the rule would see, shortly after the user stops typing.
  const key = JSON.stringify({ ...r, name: 'preview', channel_ids: [], enabled: true });
  useEffect(() => {
    const t = setTimeout(() => {
      setPreviewing(true);
      api
        .previewAlert(project, JSON.parse(key) as AlertRuleInput)
        .then(setPreview, () => setPreview(undefined))
        .finally(() => setPreviewing(false));
    }, 400);
    return () => clearTimeout(t);
  }, [project, key]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      await onSave(r);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSaving(false);
    }
  };

  return (
    <form onSubmit={submit} className="grid gap-4 xl:grid-cols-[1fr_420px]">
      <div className="flex flex-col gap-4">
        <Card>
          <CardHeader title="Condition" description={help} />
          <div className="grid gap-3 p-4 sm:grid-cols-2">
            <Field label="Name">
              <Input value={r.name} onChange={(e) => set({ name: e.target.value })} required maxLength={200} placeholder="High error rate on checkout" aria-label="Name" />
            </Field>
            <Field label="Metric">
              <Select value={r.metric} onChange={(e) => set({ metric: e.target.value as AlertRuleInput['metric'] })} aria-label="Metric">
                {metrics.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </Select>
            </Field>
            {!isEvent && (
              <>
                <Field label="Fire when the value is">
                  <div className="flex gap-2">
                    <Select value={r.comparison} onChange={(e) => set({ comparison: e.target.value as AlertRuleInput['comparison'] })} aria-label="Comparison">
                      {comparisons.map((c) => (
                        <option key={c.value} value={c.value}>
                          {c.label}
                        </option>
                      ))}
                    </Select>
                    <Input type="number" min={0} step="any" value={r.threshold ?? 0} onChange={(e) => set({ threshold: Number(e.target.value) })} aria-label="Threshold" className="w-28" />
                    <span className="self-center text-xs text-muted">{preview?.unit}</span>
                  </div>
                </Field>
                <Field label="Over the last">
                  <Select value={r.window_seconds} onChange={(e) => set({ window_seconds: Number(e.target.value) })} aria-label="Window">
                    {windows.map((w) => (
                      <option key={w} value={w}>
                        {span(w)}
                      </option>
                    ))}
                  </Select>
                </Field>
                <Field label="Check every">
                  <Select value={r.interval_seconds} onChange={(e) => set({ interval_seconds: Number(e.target.value) })} aria-label="Interval">
                    {intervals.map((w) => (
                      <option key={w} value={w}>
                        {span(w)}
                      </option>
                    ))}
                  </Select>
                </Field>
              </>
            )}
            {isEvent && (
              <Field label="Resolve after" help="The alert resolves once no new events arrive for this long.">
                <Select value={r.window_seconds} onChange={(e) => set({ window_seconds: Number(e.target.value) })} aria-label="Window">
                  {windows.map((w) => (
                    <option key={w} value={w}>
                      {span(w)}
                    </option>
                  ))}
                </Select>
              </Field>
            )}
            {r.metric === 'log_count' && (
              <Field label="Log search" help='Same syntax as the logs explorer, e.g. level:error+ "payment failed"'>
                <Input value={r.log_query ?? ''} onChange={(e) => set({ log_query: e.target.value })} placeholder="level:error+" aria-label="Log search" className="font-mono" />
              </Field>
            )}
            {r.metric === 'event_count' && (
              <Field label="Event name">
                <Input value={r.event_name} onChange={(e) => set({ event_name: e.target.value })} required placeholder="checkout_started" aria-label="Event name" />
              </Field>
            )}
          </div>
        </Card>
        <Card>
          <CardHeader title="Filters" description="Leave empty to match everything." />
          <div className="grid gap-3 p-4 sm:grid-cols-3">
            <Field label="Service">
              <Input value={r.service} onChange={(e) => set({ service: e.target.value })} placeholder="any" aria-label="Service" />
            </Field>
            <Field label="Route" help="Server route, or a URL fragment for browser errors">
              <Input value={r.route} onChange={(e) => set({ route: e.target.value })} placeholder="any" aria-label="Route" />
            </Field>
            <Field label="Environment">
              <Input value={r.environment} onChange={(e) => set({ environment: e.target.value })} placeholder="any" aria-label="Environment" />
            </Field>
          </div>
        </Card>
        <Card>
          <CardHeader
            title="Notify"
            actions={
              <Link href="/alerts/channels" className="text-xs text-primary hover:underline">
                Manage channels →
              </Link>
            }
          />
          <div className="flex flex-col gap-2 p-4">
            {channels?.length === 0 && <p className="text-sm text-muted">No channels yet. Add an email, webhook or Slack channel first.</p>}
            {channels?.map((c) => (
              <label key={c.id} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={r.channel_ids?.includes(c.id) ?? false}
                  onChange={(e) => set({ channel_ids: e.target.checked ? [...(r.channel_ids ?? []), c.id] : (r.channel_ids ?? []).filter((x) => x !== c.id) })}
                  data-testid="rule-channel"
                />
                {c.name}
                <span className="text-xs text-muted">
                  {c.kind} · {c.target}
                </span>
              </label>
            ))}
            <div className="mt-2 grid gap-3 sm:grid-cols-2">
              <Field label="Repeat while firing" help="Also stops a flapping rule from paging again within this time.">
                <Select value={r.renotify_seconds} onChange={(e) => set({ renotify_seconds: Number(e.target.value) })} aria-label="Repeat">
                  <option value={0}>Never</option>
                  {[900, 3600, 4 * 3600, 86400].map((w) => (
                    <option key={w} value={w}>
                      Every {span(w)}
                    </option>
                  ))}
                </Select>
              </Field>
              <label className="flex items-center gap-2 self-end text-sm">
                <input type="checkbox" checked={r.enabled ?? true} onChange={(e) => set({ enabled: e.target.checked })} />
                Enabled
              </label>
            </div>
          </div>
        </Card>
        <ErrorNote error={error} />
        <div className="flex gap-2">
          <Button type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Save rule'}
          </Button>
          <Link href="/alerts" className="inline-flex h-9 items-center px-3 text-sm text-muted hover:text-fg">
            Cancel
          </Link>
        </div>
      </div>
      <Card className="h-fit xl:sticky xl:top-6">
        <CardHeader
          title="Preview"
          description={preview?.supported ? `Each point is one ${span(preview.bucket_seconds)} bucket. Now: ${value(preview.current, preview.unit)}` : undefined}
          actions={previewing && <Spinner />}
        />
        <div className="p-3">{preview ? <AlertChart preview={preview} height={200} /> : <p className="py-10 text-center text-sm text-muted">Loading…</p>}</div>
      </Card>
    </form>
  );
}
