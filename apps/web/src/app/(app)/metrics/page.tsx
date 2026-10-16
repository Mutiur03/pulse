'use client';
import type { MetricResult, QueryMetricQuery } from '@pulse/api-client';
import { api } from '@/lib/api';
import { fromRange } from '@/lib/format';
import { useAsync, useQueryState } from '@/lib/hooks';
import { TimeRange } from '@/components/time-range';
import { Card, CardHeader, Empty, ErrorNote, Input, Loading, PageHeader, Select, Spinner } from '@/components/ui';

const aggs = ['', 'avg', 'sum', 'min', 'max', 'count', 'rate', 'p50', 'p90', 'p95', 'p99'] as const;
const colors = ['#a78bfa', '#38bdf8', '#34d399', '#fbbf24', '#f87171', '#f472b6', '#94a3b8', '#2dd4bf'];

function fmt(v: number, unit: string): string {
  const n = Math.abs(v) >= 100 ? Math.round(v) : Math.round(v * 100) / 100;
  return `${n.toLocaleString()}${unit && unit !== '1' ? ` ${unit}` : ''}`;
}

function Chart({ res, width = 900, height = 260 }: { res: MetricResult; width?: number; height?: number }) {
  const pad = { l: 64, r: 12, t: 12, b: 22 };
  const all = res.series.flatMap((s) => s.points);
  if (all.length === 0) return <p className="px-4 py-16 text-center text-sm text-muted">No data points in this range.</p>;
  const ts = all.map((p) => Date.parse(p.t));
  const t0 = Math.min(...ts);
  const t1 = Math.max(...ts, t0 + 1);
  const vs = all.map((p) => p.v);
  const lo = Math.min(0, ...vs);
  const hi = Math.max(...vs, lo + 1e-9);
  const x = (t: string) => pad.l + ((Date.parse(t) - t0) / (t1 - t0)) * (width - pad.l - pad.r);
  const y = (v: number) => pad.t + (1 - (v - lo) / (hi - lo)) * (height - pad.t - pad.b);
  const clock = (t: number) => new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full" role="img" aria-label={`${res.name} over time`} data-testid="metric-chart">
      {[0, 0.5, 1].map((f) => (
        <g key={f}>
          <line x1={pad.l} x2={width - pad.r} y1={y(lo + (hi - lo) * f)} y2={y(lo + (hi - lo) * f)} className="stroke-zinc-800" />
          <text x={pad.l - 6} y={y(lo + (hi - lo) * f) + 3} textAnchor="end" className="fill-zinc-500 text-[10px]">
            {fmt(lo + (hi - lo) * f, res.unit)}
          </text>
        </g>
      ))}
      {res.series.map((s, i) => (
        <g key={s.label || i} data-testid="metric-series">
          <path
            d={s.points.map((p, j) => `${j ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join(' ')}
            fill="none"
            stroke={colors[i % colors.length]}
            strokeWidth={1.5}
          />
          {s.points.map((p) => (
            <circle key={p.t} cx={x(p.t)} cy={y(p.v)} r={2} fill={colors[i % colors.length]}>
              <title>{`${s.label || res.name} ${new Date(p.t).toLocaleTimeString()}: ${fmt(p.v, res.unit)}`}</title>
            </circle>
          ))}
        </g>
      ))}
      <text x={pad.l} y={height - 4} className="fill-zinc-500 text-[10px]">
        {clock(t0)}
      </text>
      <text x={width - pad.r} y={height - 4} textAnchor="end" className="fill-zinc-500 text-[10px]">
        {clock(t1)}
      </text>
    </svg>
  );
}

export default function MetricsPage() {
  const [q, setQ] = useQueryState(['name', 'agg', 'group_by', 'range']);
  const range = q.range ?? '1h';
  const names = useAsync(() => api.listMetricNames({ from: fromRange(range) }), [range]);
  const name = q.name || names.data?.[0]?.name;
  const meta = names.data?.find((m) => m.name === name);
  const result = useAsync(
    () =>
      name
        ? api.queryMetric({
            name,
            from: fromRange(range),
            agg: (q.agg || undefined) as QueryMetricQuery['agg'],
            group_by: q.group_by || undefined,
          })
        : Promise.resolve(undefined),
    [name, range, q.agg, q.group_by],
  );

  return (
    <>
      <PageHeader title="Metrics" description="OTLP metrics from your services, over HTTP or gRPC." />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Select aria-label="Metric" value={name ?? ''} onChange={(e) => setQ({ name: e.target.value })} data-testid="metric-select">
          {names.data?.map((m) => (
            <option key={m.name} value={m.name}>
              {m.name}
            </option>
          ))}
        </Select>
        <Select aria-label="Aggregation" value={q.agg ?? ''} onChange={(e) => setQ({ agg: e.target.value })}>
          {aggs.map((a) => (
            <option key={a} value={a}>
              {a ? a : 'default aggregation'}
            </option>
          ))}
        </Select>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setQ({ group_by: String(new FormData(e.currentTarget).get('group_by') ?? '').trim() });
          }}
        >
          <Input name="group_by" aria-label="Group by" placeholder="Group by: service.name or attribute" defaultValue={q.group_by ?? ''} className="w-72" />
        </form>
        <TimeRange value={range} onChange={(v) => setQ({ range: v })} />
      </div>
      <ErrorNote error={names.error ?? result.error} />
      {names.loading && !names.data && (
        <Card>
          <Loading rows={3} />
        </Card>
      )}
      {names.data?.length === 0 && (
        <Card>
          <Empty title="No metrics yet" action={{ label: 'Set up data collection', href: '/setup' }}>
            Point an OTLP metrics exporter at the ingest gateway (:4317 for gRPC, :8080/v1/metrics for HTTP).
          </Empty>
        </Card>
      )}
      {name && (
        <Card>
          <CardHeader
            title={name}
            description={
              result.data
                ? `${result.data.type || meta?.type || ''} · ${result.data.agg} per ${result.data.step}s${meta ? ` · ${meta.services.join(', ')}` : ''}`
                : undefined
            }
          />
          <div className="p-4">{result.data ? <Chart res={result.data} /> : <Spinner />}</div>
          {result.data && result.data.series.length > 1 && (
            <div className="flex flex-wrap gap-4 border-t border-border px-4 py-3 text-xs">
              {result.data.series.map((s, i) => (
                <span key={s.label} className="flex items-center gap-1.5">
                  <span className="inline-block h-2 w-2 rounded-full" style={{ background: colors[i % colors.length] }} />
                  {s.label || '(none)'}
                </span>
              ))}
            </div>
          )}
        </Card>
      )}
    </>
  );
}
