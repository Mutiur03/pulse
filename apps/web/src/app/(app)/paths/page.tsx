'use client';
import { useEffect, useRef, useState } from 'react';
import type { PathsQuery, PathsResult } from '@pulse/api-client';
import { api, handleAuthError } from '@/lib/api';
import { Button, Card, CardHeader, Empty, ErrorNote, Input, PageHeader, Select, Spinner } from '@/components/ui';
import { palette } from '@/components/charts';

/** Steps as columns, nodes as bars sized by sessions, links as curves. */
function Flow({ res }: { res: PathsResult }) {
  const colW = 220;
  const nodeW = 12;
  const gap = 10;
  const height = 420;
  const steps = Math.max(...res.nodes.map((n) => n.step), 0) + 1;
  const width = steps * colW;
  const perStep = Array.from({ length: steps }, (_, s) => res.nodes.filter((n) => n.step === s));
  const max = Math.max(...perStep.map((ns) => ns.reduce((a, n) => a + n.sessions, 0) + gap * ns.length), 1);
  const scale = (height - 20) / max;
  const pos = new Map<string, { x: number; y: number; h: number; outY: number; inY: number }>();
  perStep.forEach((ns, s) => {
    let y = 10;
    for (const n of ns) {
      const h = Math.max(n.sessions * scale, 2);
      pos.set(`${s}|${n.name}`, { x: s * colW + 4, y, h, outY: y, inY: y });
      y += h + gap;
    }
  });
  const colorOf = (name: string) => palette[[...name].reduce((a, c) => a + c.charCodeAt(0), 0) % palette.length];
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full" role="img" aria-label="user paths" data-testid="paths-flow">
      {res.links.map((l, i) => {
        const a = pos.get(`${l.step}|${l.source}`);
        const b = pos.get(`${l.step + 1}|${l.target}`);
        if (!a || !b) return null;
        const h = Math.max(l.sessions * scale, 1);
        const y1 = a.outY + h / 2;
        const y2 = b.inY + h / 2;
        a.outY += h;
        b.inY += h;
        const x1 = a.x + nodeW;
        const x2 = b.x;
        const mx = (x1 + x2) / 2;
        return (
          <path key={i} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} fill="none" stroke={colorOf(l.source)} strokeOpacity={0.35} strokeWidth={h} data-testid="path-link">
            <title>{`${l.source} → ${l.target}: ${l.sessions} sessions`}</title>
          </path>
        );
      })}
      {res.nodes.map((n) => {
        const p = pos.get(`${n.step}|${n.name}`)!;
        return (
          <g key={`${n.step}|${n.name}`} data-testid="path-node">
            <rect x={p.x} y={p.y} width={nodeW} height={p.h} rx={2} fill={colorOf(n.name)}>
              <title>{`${n.name}: ${n.sessions} sessions, ${n.dropoff} ended here`}</title>
            </rect>
            <text x={p.x + nodeW + 6} y={p.y + Math.min(p.h / 2, 14) + 4} className="fill-zinc-200 text-[11px]">
              {n.name.length > 26 ? `${n.name.slice(0, 25)}…` : n.name}
              <tspan className="fill-zinc-500"> {n.sessions}</tspan>
            </text>
          </g>
        );
      })}
    </svg>
  );
}

export default function PathsPage() {
  const [q, setQ] = useState<PathsQuery>({ mode: 'pageview', start: '', steps: 4 });
  const [res, setRes] = useState<PathsResult>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const seq = useRef(0);
  const run = async (query = q) => {
    const id = ++seq.current;
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.queryPaths({ ...query, start: query.start?.trim() || undefined });
      if (id === seq.current) setRes(r);
    } catch (e) {
      handleAuthError(e);
      if (id === seq.current) setError(e instanceof Error ? e.message : String(e));
    }
    if (id === seq.current) setBusy(false);
  };
  useEffect(() => {
    void run();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <>
      <PageHeader title="User paths" description="How sessions move through your app, page by page or event by event, over the last 7 days." />
      <Card className="mb-4 flex flex-wrap items-end gap-2 p-4">
        <label className="flex flex-col gap-1 text-xs text-muted">
          Follow
          <Select aria-label="Path mode" value={q.mode} onChange={(e) => setQ({ ...q, mode: e.target.value as PathsQuery['mode'] })}>
            <option value="pageview">Pages</option>
            <option value="event">Events</option>
          </Select>
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted">
          Starting at
          <Input aria-label="Path start" value={q.start ?? ''} onChange={(e) => setQ({ ...q, start: e.target.value })} placeholder={q.mode === 'event' ? 'any event' : 'any page, e.g. /pricing'} className="w-56" />
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted">
          Steps
          <Select aria-label="Path steps" value={q.steps} onChange={(e) => setQ({ ...q, steps: Number(e.target.value) })}>
            {[2, 3, 4, 5, 6, 7, 8].map((n) => (
              <option key={n}>{n}</option>
            ))}
          </Select>
        </label>
        <Button onClick={() => run()} disabled={busy} data-testid="run-paths" data-busy={busy}>
          {busy ? 'Running…' : 'Run'}
        </Button>
        {busy && <Spinner />}
      </Card>
      <ErrorNote error={error} />
      {res && res.total_sessions === 0 && (
        <Card>
          <Empty>No sessions match. Try another start {q.mode === 'event' ? 'event' : 'page'}.</Empty>
        </Card>
      )}
      {res && res.total_sessions > 0 && (
        <>
          <Card className="mb-4">
            <CardHeader title="Flow" description={`${res.total_sessions.toLocaleString()} sessions · busiest ${8} per step, the rest as Other`} />
            <div className="overflow-x-auto p-4">
              <Flow res={res} />
            </div>
          </Card>
          <Card>
            <CardHeader title="Most common paths" />
            <ol className="divide-y divide-border text-sm" data-testid="top-paths">
              {res.top_paths.map((p, i) => (
                <li key={i} className="flex items-center justify-between gap-3 px-4 py-2">
                  <span className="font-mono text-xs text-zinc-300">{p.path.join('  →  ')}</span>
                  <span className="tabular-nums text-muted">{p.sessions}</span>
                </li>
              ))}
            </ol>
          </Card>
        </>
      )}
    </>
  );
}
