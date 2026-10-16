'use client';
import Link from 'next/link';
import { useEffect, useRef, useState } from 'react';
import type { Dashboard, TrendResult } from '@pulse/api-client';
import { api, currentProjectId, handleAuthError } from '@/lib/api';
import { useAsync } from '@/lib/hooks';
import { TrendChart } from '@/components/charts';
import { TrendBuilder, trendRequest, type SavedTrend } from '@/components/query-builder';
import { dashboardRanges } from '@/components/widgets';
import { Button, Card, CardHeader, ErrorNote, Input, PageHeader, Select, Spinner } from '@/components/ui';

const initial: SavedTrend = { series: [{ event: '$pageview', math: 'total' }], interval: 'day', display: 'line' };

export default function InsightsPage() {
  const [q, setQ] = useState<SavedTrend>(initial);
  const [range, setRange] = useState('7d');
  const [result, setResult] = useState<TrendResult>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const seq = useRef(0);
  const names = useAsync(() => api.listEventNames({ from: new Date(Date.now() - 30 * 86400e3).toISOString() }), []);
  const boards = useAsync(async () => api.listDashboards(await currentProjectId()), []);

  const run = async (query = q, r = range) => {
    const id = ++seq.current;
    setBusy(true);
    setError(undefined);
    try {
      const from = new Date(Date.now() - dashboardRanges[r].ms).toISOString();
      const interval = r === '24h' ? 'hour' : query.interval;
      const res = await api.queryTrend(trendRequest({ ...query, interval }, from));
      if (id === seq.current) setResult(res);
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
      <PageHeader title="Insights" description="Chart any event over time: filters, breakdowns and formulas across series. Save the result to a dashboard." />
      <Card className="mb-4 p-4">
        <TrendBuilder value={q} onChange={setQ} events={names.data?.map((n) => n.name) ?? []} />
        <div className="mt-4 flex flex-wrap items-end gap-2 border-t border-border pt-4">
          <Select aria-label="Insight range" value={range} onChange={(e) => setRange(e.target.value)}>
            {Object.keys(dashboardRanges).map((r) => (
              <option key={r} value={r}>
                Last {r}
              </option>
            ))}
          </Select>
          <Button onClick={() => run()} disabled={busy} data-testid="run-trend" data-busy={busy}>
            {busy ? 'Running…' : 'Run query'}
          </Button>
          {busy && <Spinner />}
        </div>
      </Card>
      <ErrorNote error={error} onRetry={() => void run()} />
      {result && (
        <Card className="mb-4" data-testid="trend-result">
          <CardHeader
            title="Result"
            description={`${result.series.length} series · per ${result.interval}`}
            actions={<SaveToDashboard query={q} boards={boards.data ?? []} onSaved={boards.reload} />}
          />
          <div className="p-4">
            <TrendChart result={result} display={q.display} />
          </div>
        </Card>
      )}
    </>
  );
}

function SaveToDashboard({ query, boards, onSaved }: { query: SavedTrend; boards: Dashboard[]; onSaved: () => void }) {
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState('new');
  const [title, setTitle] = useState('');
  const [saved, setSaved] = useState<Dashboard>();
  const [error, setError] = useState<string>();
  const editable = boards.filter((b) => b.can_edit);
  if (!open)
    return (
      <Button size="sm" variant="outline" onClick={() => setOpen(true)} data-testid="save-insight">
        Save to dashboard
      </Button>
    );
  const save = async () => {
    setError(undefined);
    try {
      const project = await currentProjectId();
      const widget = { id: `w${Date.now().toString(36)}`, title: title || 'Trend', kind: 'trend' as const, width: 1, query: query as unknown as Record<string, unknown> };
      let d: Dashboard;
      if (target === 'new') {
        d = await api.createDashboard(project, { name: title || 'New dashboard', widgets: [widget] });
      } else {
        const cur = await api.getDashboard(project, Number(target));
        d = await api.updateDashboard(project, cur.id, { name: cur.name, description: cur.description, shared: cur.shared, widgets: [...cur.widgets, widget] });
      }
      setSaved(d);
      onSaved();
    } catch (e) {
      handleAuthError(e);
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  if (saved)
    return (
      <Link href={`/dashboards/${saved.id}`} className="text-sm text-primary hover:underline" data-testid="saved-link">
        Saved to {saved.name} →
      </Link>
    );
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label="Widget title" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title" className="h-8 w-44 text-xs" />
      <Select aria-label="Dashboard" value={target} onChange={(e) => setTarget(e.target.value)} className="h-8 text-xs">
        <option value="new">New dashboard</option>
        {editable.map((b) => (
          <option key={b.id} value={b.id}>
            {b.name}
          </option>
        ))}
      </Select>
      <Button size="sm" onClick={save} data-testid="confirm-save-insight">
        Save
      </Button>
      {error && <span className="text-xs text-red-300">{error}</span>}
    </div>
  );
}
