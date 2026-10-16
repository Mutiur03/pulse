import type { TrendResult } from '@pulse/api-client';

export const palette = ['#a78bfa', '#38bdf8', '#34d399', '#fbbf24', '#f87171', '#f472b6', '#94a3b8', '#2dd4bf', '#fb923c', '#c084fc'];

export function compact(v: number): string {
  const a = Math.abs(v);
  if (a >= 1e9) return `${(v / 1e9).toFixed(1)}B`;
  if (a >= 1e6) return `${(v / 1e6).toFixed(1)}M`;
  if (a >= 1e4) return `${(v / 1e3).toFixed(1)}k`;
  if (a >= 100) return Math.round(v).toLocaleString();
  return String(Math.round(v * 100) / 100);
}

export interface Line {
  label: string;
  data: number[];
}

function tick(t: string, interval: string): string {
  const d = new Date(t);
  if (interval === 'hour') return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit' });
  if (interval === 'month') return d.toLocaleDateString([], { month: 'short', year: 'numeric' });
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

/** Lines over time buckets with a legend; values on hover. */
export function LineChart({
  buckets,
  lines,
  interval = 'day',
  height = 220,
  unit = '',
  testId = 'line-chart',
  legend,
}: {
  buckets: string[];
  lines: Line[];
  interval?: string;
  height?: number;
  unit?: string;
  testId?: string;
  legend?: boolean;
}) {
  const width = 640;
  const pad = { l: 48, r: 10, t: 10, b: 22 };
  const all = lines.flatMap((l) => l.data);
  if (buckets.length === 0 || lines.length === 0) return <p className="px-4 py-12 text-center text-sm text-muted">No data in this range.</p>;
  const lo = Math.min(0, ...all);
  const hi = Math.max(...all, lo + 1);
  const n = Math.max(buckets.length - 1, 1);
  const x = (i: number) => pad.l + (i / n) * (width - pad.l - pad.r);
  const y = (v: number) => pad.t + (1 - (v - lo) / (hi - lo)) * (height - pad.t - pad.b);
  const ticks = [0, Math.floor(buckets.length / 2), buckets.length - 1].filter((v, i, a) => a.indexOf(v) === i);
  return (
    <div>
      <svg viewBox={`0 0 ${width} ${height}`} className="w-full" role="img" aria-label="chart" data-testid={testId}>
        {[0, 0.5, 1].map((f) => (
          <g key={f}>
            <line x1={pad.l} x2={width - pad.r} y1={y(lo + (hi - lo) * f)} y2={y(lo + (hi - lo) * f)} className="stroke-zinc-800" />
            <text x={pad.l - 6} y={y(lo + (hi - lo) * f) + 3} textAnchor="end" className="fill-zinc-500 text-[10px]">
              {compact(lo + (hi - lo) * f)}
              {unit}
            </text>
          </g>
        ))}
        {lines.map((l, i) => (
          <g key={`${l.label}-${i}`} data-testid="chart-line">
            <path
              d={l.data.map((v, j) => `${j ? 'L' : 'M'}${x(j).toFixed(1)},${y(v).toFixed(1)}`).join(' ')}
              fill="none"
              stroke={palette[i % palette.length]}
              strokeWidth={1.75}
            />
            {l.data.map((v, j) => (
              <circle key={j} cx={x(j)} cy={y(v)} r={buckets.length > 60 ? 1 : 2.25} fill={palette[i % palette.length]}>
                <title>{`${l.label} · ${tick(buckets[j], interval)}: ${compact(v)}${unit}`}</title>
              </circle>
            ))}
          </g>
        ))}
        {ticks.map((i) => (
          <text key={i} x={x(i)} y={height - 5} textAnchor={i === 0 ? 'start' : i === buckets.length - 1 ? 'end' : 'middle'} className="fill-zinc-500 text-[10px]">
            {tick(buckets[i], interval)}
          </text>
        ))}
      </svg>
      {(legend ?? lines.length > 1) && <Legend labels={lines.map((l) => l.label)} />}
    </div>
  );
}

export function Legend({ labels }: { labels: string[] }) {
  return (
    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-zinc-300" data-testid="chart-legend">
      {labels.map((l, i) => (
        <span key={`${l}-${i}`} className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 rounded-full" style={{ background: palette[i % palette.length] }} />
          {l}
        </span>
      ))}
    </div>
  );
}

/** Horizontal bars of totals. */
export function BarList({ items, testId = 'bar-list' }: { items: { label: string; value: number }[]; testId?: string }) {
  const max = Math.max(...items.map((i) => i.value), 1);
  if (items.length === 0) return <p className="px-4 py-12 text-center text-sm text-muted">No data in this range.</p>;
  return (
    <div className="flex flex-col gap-1.5" data-testid={testId}>
      {items.map((it, i) => (
        <div key={`${it.label}-${i}`} className="grid grid-cols-[minmax(0,12rem)_1fr_4rem] items-center gap-3 text-xs">
          <span className="truncate text-zinc-300" title={it.label}>
            {it.label}
          </span>
          <span className="h-4 rounded-sm bg-zinc-800">
            <span className="block h-4 rounded-sm" style={{ width: `${(it.value / max) * 100}%`, background: palette[i % palette.length] }} />
          </span>
          <span className="text-right tabular-nums">{compact(it.value)}</span>
        </div>
      ))}
    </div>
  );
}

/** Legend label of a trend series, with its breakdown term. */
export function seriesLabel(s: TrendResult['series'][number]): string {
  if (!s.breakdown) return s.label;
  const term = s.breakdown === '$other' ? 'Other' : s.breakdown;
  return `${s.label} · ${term}`;
}

export type TrendDisplay = 'line' | 'bar' | 'number' | 'table';

/** A trend result as a line chart, bar list, big number or table. */
export function TrendChart({ result, display = 'line', height }: { result: TrendResult; display?: TrendDisplay; height?: number }) {
  const lines = result.series.map((s) => ({ label: seriesLabel(s), data: s.data }));
  if (display === 'number') {
    const first = result.series[0];
    return (
      <div className="py-6 text-center" data-testid="trend-number">
        <div className="text-4xl font-semibold tabular-nums">{first ? compact(first.total) : '0'}</div>
        <div className="mt-1 text-xs text-muted">{first ? seriesLabel(first) : 'No data'}</div>
      </div>
    );
  }
  if (display === 'bar') return <BarList items={result.series.map((s) => ({ label: seriesLabel(s), value: s.total }))} testId="trend-bars" />;
  if (display === 'table')
    return (
      <div className="overflow-x-auto" data-testid="trend-table">
        <table className="w-full text-xs">
          <thead>
            <tr className="text-left text-muted">
              <th className="py-1 pr-4 font-medium">Series</th>
              <th className="py-1 text-right font-medium">Total</th>
            </tr>
          </thead>
          <tbody>
            {result.series.map((s, i) => (
              <tr key={i} className="border-t border-border">
                <td className="py-1.5 pr-4">{seriesLabel(s)}</td>
                <td className="py-1.5 text-right tabular-nums">{compact(s.total)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  return <LineChart buckets={result.buckets} lines={lines} interval={result.interval} height={height} testId="trend-chart" legend={lines.length > 1 || result.series.some((s) => s.breakdown)} />;
}
