'use client';
import { useState } from 'react';
import type { DashboardWidget, MetricResult, QueryMetricQuery, TrendInterval } from '@pulse/api-client';
import { api } from '@/lib/api';
import { pct } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { BarList, LineChart, TrendChart, compact } from './charts';
import { TrendBuilder, trendRequest, type SavedTrend } from './query-builder';
import { Button, Card, CardHeader, ErrorNote, Input, Select, Spinner } from './ui';

export type WidgetKind = DashboardWidget['kind'];
export const kindLabels: Record<WidgetKind, string> = { trend: 'Trend', funnel: 'Funnel', metric: 'Metric', traces: 'Trace stats' };

export const dashboardRanges: Record<string, { ms: number; interval: TrendInterval }> = {
  '24h': { ms: 86400e3, interval: 'hour' },
  '7d': { ms: 7 * 86400e3, interval: 'day' },
  '30d': { ms: 30 * 86400e3, interval: 'day' },
};

export interface FunnelSaved {
  steps: string[];
  window_seconds?: number;
}
export interface MetricSaved {
  name: string;
  agg?: string;
  group_by?: string;
}
export interface TracesSaved {
  service?: string;
}

export function defaultQuery(kind: WidgetKind, events: string[]): Record<string, unknown> {
  switch (kind) {
    case 'trend':
      return { series: [{ event: events.includes('$pageview') ? '$pageview' : (events[0] ?? ''), math: 'total' }], interval: 'day', display: 'line' } satisfies SavedTrend;
    case 'funnel':
      return { steps: events.slice(0, 2).length === 2 ? events.slice(0, 2) : ['$pageview', '$pageview'], window_seconds: 3600 };
    case 'metric':
      return { name: '' };
    default:
      return {};
  }
}

/** Every bucket of every metric series on the first series' timeline. */
function metricLines(res: MetricResult) {
  const times = Array.from(new Set(res.series.flatMap((s) => s.points.map((p) => p.t)))).sort();
  return {
    buckets: times,
    lines: res.series.map((s) => {
      const by = new Map(s.points.map((p) => [p.t, p.v]));
      return { label: s.label || res.name, data: times.map((t) => by.get(t) ?? 0) };
    }),
  };
}

function Body({ widget, range }: { widget: DashboardWidget; range: string }) {
  const r = dashboardRanges[range] ?? dashboardRanges['7d'];
  const q = (widget.query ?? {}) as Record<string, unknown>;
  const key = JSON.stringify([widget.kind, q, range]);
  const res = useAsync(async () => {
    const from = new Date(Date.now() - r.ms).toISOString();
    switch (widget.kind) {
      case 'trend': {
        const t = q as unknown as SavedTrend;
        const interval = range === '24h' ? 'hour' : (t.interval ?? r.interval);
        return { kind: 'trend' as const, data: await api.queryTrend(trendRequest({ ...t, interval }, from)) };
      }
      case 'funnel': {
        const f = q as unknown as FunnelSaved;
        return { kind: 'funnel' as const, data: await api.queryFunnel({ steps: f.steps, window_seconds: f.window_seconds ?? 3600, from }) };
      }
      case 'metric': {
        const m = q as unknown as MetricSaved;
        return {
          kind: 'metric' as const,
          data: await api.queryMetric({ name: m.name, from, agg: (m.agg || undefined) as QueryMetricQuery['agg'], group_by: m.group_by || undefined }),
        };
      }
      default: {
        const t = q as TracesSaved;
        return { kind: 'traces' as const, data: await api.traceStats({ from, interval: r.interval, service: t.service || undefined }) };
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  if (res.error) return <ErrorNote error={res.error} />;
  if (!res.data)
    return (
      <div className="grid h-40 place-items-center">
        <Spinner />
      </div>
    );
  const d = res.data;
  if (d.kind === 'trend') return <TrendChart result={d.data} display={(q as SavedTrend).display} height={180} />;
  if (d.kind === 'funnel') {
    const top = d.data.steps[0]?.users ?? 0;
    return (
      <div data-testid="widget-funnel">
        <BarList items={d.data.steps.map((s, i) => ({ label: `${i + 1}. ${s.event}`, value: s.users }))} />
        <p className="mt-2 text-xs text-muted">
          {top.toLocaleString()} entered · {pct(d.data.steps[d.data.steps.length - 1]?.conversion_overall ?? 0)} converted
        </p>
      </div>
    );
  }
  if (d.kind === 'metric') {
    const { buckets, lines } = metricLines(d.data);
    return <LineChart buckets={buckets} lines={lines} interval={range === '24h' ? 'hour' : 'day'} height={180} unit={d.data.unit && d.data.unit !== '1' ? ` ${d.data.unit}` : ''} testId="widget-metric" />;
  }
  const s = d.data;
  const p95 = Math.max(0, ...s.points.map((p) => p.p95_ms));
  return (
    <div data-testid="widget-traces">
      <div className="mb-2 grid grid-cols-3 gap-2 text-center">
        <Stat label="Requests" value={compact(s.requests)} />
        <Stat label="Error rate" value={s.requests ? pct(s.errors / s.requests) : '0%'} />
        <Stat label="Worst p95" value={`${compact(p95)} ms`} />
      </div>
      <LineChart
        buckets={s.points.map((p) => p.t)}
        lines={[
          { label: 'Requests', data: s.points.map((p) => p.requests) },
          { label: 'Errors', data: s.points.map((p) => p.errors) },
        ]}
        interval={s.interval}
        height={140}
      />
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-zinc-900/60 px-2 py-1.5">
      <div className="text-base font-semibold tabular-nums">{value}</div>
      <div className="text-[11px] text-muted">{label}</div>
    </div>
  );
}

export function WidgetCard({
  widget,
  range,
  editing,
  onEdit,
  onRemove,
}: {
  widget: DashboardWidget;
  range: string;
  editing?: boolean;
  onEdit?: () => void;
  onRemove?: () => void;
}) {
  return (
    <Card className={widget.width === 2 ? 'lg:col-span-2' : ''} data-testid="widget" data-kind={widget.kind}>
      <CardHeader
        title={widget.title || kindLabels[widget.kind]}
        description={kindLabels[widget.kind]}
        actions={
          editing && (
            <div className="flex gap-1">
              <Button size="sm" variant="ghost" onClick={onEdit} data-testid="edit-widget">
                Edit
              </Button>
              <Button size="sm" variant="ghost" onClick={onRemove} data-testid="remove-widget">
                Remove
              </Button>
            </div>
          )
        }
      />
      <div className="p-4">
        <Body widget={widget} range={range} />
      </div>
    </Card>
  );
}

/** Configures a widget: kind, title, width and its query. */
export function WidgetEditor({
  initial,
  events,
  metrics,
  services,
  onSave,
  onCancel,
}: {
  initial: DashboardWidget;
  events: string[];
  metrics: string[];
  services: string[];
  onSave: (w: DashboardWidget) => void;
  onCancel: () => void;
}) {
  const [w, setW] = useState<DashboardWidget>(initial);
  const q = (w.query ?? {}) as Record<string, unknown>;
  const setQ = (patch: Record<string, unknown>) => setW({ ...w, query: { ...q, ...patch } });
  const funnel = q as unknown as FunnelSaved;
  const metric = q as unknown as MetricSaved;
  return (
    <Card className="mb-4 p-4" data-testid="widget-editor">
      <div className="mb-4 flex flex-wrap items-end gap-2">
        <label className="flex flex-col gap-1 text-xs text-muted">
          Type
          <Select
            aria-label="Widget type"
            value={w.kind}
            onChange={(e) => {
              const kind = e.target.value as WidgetKind;
              const query = defaultQuery(kind, events);
              if (kind === 'metric' && metrics[0]) query.name = metrics[0];
              setW({ ...w, kind, query });
            }}
          >
            {Object.entries(kindLabels).map(([k, l]) => (
              <option key={k} value={k}>
                {l}
              </option>
            ))}
          </Select>
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted">
          Title
          <Input aria-label="Widget title" value={w.title ?? ''} onChange={(e) => setW({ ...w, title: e.target.value })} placeholder={kindLabels[w.kind]} className="w-64" />
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted">
          Width
          <Select aria-label="Widget width" value={w.width ?? 1} onChange={(e) => setW({ ...w, width: Number(e.target.value) })}>
            <option value={1}>Half</option>
            <option value={2}>Full</option>
          </Select>
        </label>
      </div>
      {w.kind === 'trend' && <TrendBuilder value={q as unknown as SavedTrend} onChange={(t) => setW({ ...w, query: t as unknown as Record<string, unknown> })} events={events} />}
      {w.kind === 'funnel' && (
        <div className="flex flex-wrap items-end gap-2">
          {funnel.steps.map((s, i) => (
            <Select key={i} aria-label={`Funnel step ${i + 1}`} value={s} onChange={(e) => setQ({ steps: funnel.steps.map((x, j) => (j === i ? e.target.value : x)) })}>
              {Array.from(new Set([...events, s])).map((o) => (
                <option key={o}>{o}</option>
              ))}
            </Select>
          ))}
          {funnel.steps.length < 5 && (
            <Button variant="outline" onClick={() => setQ({ steps: [...funnel.steps, events[0] ?? '$pageview'] })}>
              + Step
            </Button>
          )}
          {funnel.steps.length > 2 && (
            <Button variant="ghost" onClick={() => setQ({ steps: funnel.steps.slice(0, -1) })}>
              Remove last step
            </Button>
          )}
        </div>
      )}
      {w.kind === 'metric' && (
        <div className="flex flex-wrap items-end gap-2">
          <Select aria-label="Metric name" value={metric.name} onChange={(e) => setQ({ name: e.target.value })}>
            <option value="">Pick a metric</option>
            {Array.from(new Set([...metrics, metric.name].filter(Boolean))).map((m) => (
              <option key={m}>{m}</option>
            ))}
          </Select>
          <Select aria-label="Metric aggregation" value={metric.agg ?? ''} onChange={(e) => setQ({ agg: e.target.value })}>
            {['', 'avg', 'sum', 'min', 'max', 'count', 'rate', 'p50', 'p95', 'p99'].map((a) => (
              <option key={a} value={a}>
                {a || 'default aggregation'}
              </option>
            ))}
          </Select>
          <Input aria-label="Metric group by" value={metric.group_by ?? ''} onChange={(e) => setQ({ group_by: e.target.value })} placeholder="group by (optional)" />
        </div>
      )}
      {w.kind === 'traces' && (
        <Select aria-label="Service" value={(q as TracesSaved).service ?? ''} onChange={(e) => setQ({ service: e.target.value })}>
          <option value="">All services</option>
          {services.map((s) => (
            <option key={s}>{s}</option>
          ))}
        </Select>
      )}
      <div className="mt-4 flex gap-2 border-t border-border pt-4">
        <Button onClick={() => onSave(w)} data-testid="save-widget">
          Save widget
        </Button>
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </Card>
  );
}
