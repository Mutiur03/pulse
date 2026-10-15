import type { AlertPreview } from '@pulse/api-client';

/** The metric over time with the threshold as a dashed line; breaching points are red. */
export function AlertChart({ preview, width = 640, height = 160 }: { preview: AlertPreview; width?: number; height?: number }) {
  const pad = { l: 40, r: 8, t: 10, b: 20 };
  const pts = preview.points;
  if (!preview.supported) return <p className="px-4 py-10 text-center text-sm text-muted">This rule fires on issue events, so there is no metric to chart.</p>;
  if (pts.length === 0) return <p className="px-4 py-10 text-center text-sm text-muted">No data for this metric yet.</p>;
  const max = Math.max(preview.threshold * 1.25, ...pts.map((p) => p.value), 1);
  const t0 = Date.parse(pts[0].at);
  const t1 = Math.max(Date.parse(pts[pts.length - 1].at), t0 + 1);
  const x = (at: string) => pad.l + ((Date.parse(at) - t0) / (t1 - t0)) * (width - pad.l - pad.r);
  const y = (v: number) => pad.t + (1 - v / max) * (height - pad.t - pad.b);
  const breach = (v: number) =>
    preview.comparison === 'gt' ? v > preview.threshold : preview.comparison === 'gte' ? v >= preview.threshold : preview.comparison === 'lt' ? v < preview.threshold : v <= preview.threshold;
  const line = pts.map((p, i) => `${i ? 'L' : 'M'}${x(p.at).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ');
  const fmt = (v: number) => `${Math.round(v * 100) / 100}${preview.unit}`;
  const clock = (at: string) => new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full" role="img" aria-label="metric against threshold" data-testid="alert-chart">
      {[0, 0.5, 1].map((f) => (
        <g key={f}>
          <line x1={pad.l} x2={width - pad.r} y1={y(max * f)} y2={y(max * f)} className="stroke-zinc-800" />
          <text x={pad.l - 4} y={y(max * f) + 3} textAnchor="end" className="fill-zinc-500 text-[9px]">
            {fmt(max * f)}
          </text>
        </g>
      ))}
      <path d={line} fill="none" className="stroke-primary" strokeWidth={1.5} />
      {pts.map((p) => (
        <circle key={p.at} cx={x(p.at)} cy={y(p.value)} r={2.5} className={breach(p.value) ? 'fill-red-400' : 'fill-primary'}>
          <title>{`${clock(p.at)}: ${fmt(p.value)}`}</title>
        </circle>
      ))}
      <line x1={pad.l} x2={width - pad.r} y1={y(preview.threshold)} y2={y(preview.threshold)} className="stroke-red-400" strokeDasharray="4 3" data-testid="threshold-line" />
      <text x={width - pad.r} y={y(preview.threshold) - 4} textAnchor="end" className="fill-red-300 text-[9px]">
        threshold {fmt(preview.threshold)}
      </text>
      <text x={pad.l} y={height - 4} className="fill-zinc-500 text-[9px]">
        {clock(pts[0].at)}
      </text>
      <text x={width - pad.r} y={height - 4} textAnchor="end" className="fill-zinc-500 text-[9px]">
        {clock(pts[pts.length - 1].at)}
      </text>
    </svg>
  );
}
