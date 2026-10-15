'use client';
import Link from 'next/link';
import { useEffect, useRef, useState } from 'react';
import type { CohortHealth, FunnelResult } from '@pulse/api-client';
import { api, handleAuthError } from '@/lib/api';
import { fromRange, pct } from '@/lib/format';
import { useAsync } from '@/lib/hooks';
import { cn } from '@/lib/cn';
import { Badge, Button, Card, CardHeader, Empty, ErrorNote, PageHeader, Select, Spinner } from '@/components/ui';
import { TimeRange } from '@/components/time-range';

const windows: [number, string][] = [
  [300, '5 minutes'],
  [1800, '30 minutes'],
  [3600, '1 hour'],
  [86400, '1 day'],
  [7 * 86400, '7 days'],
];
const defaults = ['$pageview', 'checkout_started', 'payment_succeeded'];

export default function FunnelsPage() {
  const [range, setRange] = useState('7d');
  const names = useAsync(() => api.listEventNames({ from: fromRange(range) }), [range]);
  const [steps, setSteps] = useState<string[]>(defaults);
  const [win, setWin] = useState(3600);
  const [result, setResult] = useState<FunnelResult>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [focus, setFocus] = useState(0);
  const seq = useRef(0);

  const run = async (s = steps) => {
    const id = ++seq.current;
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.queryFunnel({ steps: s, window_seconds: win, from: fromRange(range) });
      if (id !== seq.current) return; // a newer run is in flight
      setResult(r);
      // Focus the step with the largest drop-off.
      let worst = 0;
      r.steps.slice(0, -1).forEach((st, i) => {
        if ((st.dropped?.users ?? 0) > (r.steps[worst].dropped?.users ?? 0)) worst = i;
      });
      setFocus(worst);
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

  const options = Array.from(new Set([...(names.data?.map((n) => n.name) ?? []), ...steps]));
  const top = result?.steps[0]?.users ?? 0;

  return (
    <>
      <PageHeader
        title="Funnels"
        description="Where users drop off, and what broke for them: errors and backend latency in the sessions of users who did not convert."
      />
      <Card className="mb-4 p-4">
        <div className="flex flex-wrap items-end gap-2">
          {steps.map((s, i) => (
            <div key={i} className="flex items-end gap-2">
              <label className="flex flex-col gap-1 text-xs text-muted">
                Step {i + 1}
                <div className="flex">
                  <Select
                    aria-label={`Step ${i + 1}`}
                    value={s}
                    onChange={(e) => setSteps(steps.map((x, j) => (j === i ? e.target.value : x)))}
                    className="w-48 rounded-r-none"
                  >
                    {options.map((o) => (
                      <option key={o}>{o}</option>
                    ))}
                  </Select>
                  <button
                    aria-label={`Remove step ${i + 1}`}
                    disabled={steps.length <= 2}
                    onClick={() => setSteps(steps.filter((_, j) => j !== i))}
                    className="h-9 rounded-r-md border border-l-0 border-border px-2 text-muted hover:text-zinc-100 disabled:opacity-30"
                  >
                    ×
                  </button>
                </div>
              </label>
              {i < steps.length - 1 && <span className="pb-2 text-muted">→</span>}
            </div>
          ))}
          {steps.length < 5 && (
            <Button variant="outline" onClick={() => setSteps([...steps, options.find((o) => !steps.includes(o)) ?? options[0]])}>
              + Step
            </Button>
          )}
        </div>
        <div className="mt-4 flex flex-wrap items-end gap-2 border-t border-border pt-4">
          <label className="flex flex-col gap-1 text-xs text-muted">
            Conversion window
            <Select aria-label="Conversion window" value={win} onChange={(e) => setWin(Number(e.target.value))}>
              {windows.map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted">
            Users from
            <TimeRange value={range} onChange={setRange} />
          </label>
          <Button onClick={() => run()} disabled={busy} data-testid="run-funnel" data-busy={busy}>
            {busy ? 'Running…' : 'Run funnel'}
          </Button>
          {busy && <Spinner />}
        </div>
      </Card>
      <ErrorNote error={error} />
      {result && top === 0 && (
        <Card>
          <Empty>No users entered this funnel in the selected time range.</Empty>
        </Card>
      )}
      {result && top > 0 && (
        <div className="grid gap-4 2xl:grid-cols-[1fr_1fr]">
          <Card data-testid="funnel-chart">
            <CardHeader
              title="Conversion"
              description={`${top.toLocaleString()} users entered · ${pct(result.steps[result.steps.length - 1].conversion_overall)} converted`}
            />
            <div className="flex h-72 items-end gap-3 px-6 pb-4 pt-8">
              {result.steps.map((s, i) => (
                <button
                  key={i}
                  onClick={() => setFocus(i)}
                  disabled={i === result.steps.length - 1}
                  className="group flex h-full flex-1 flex-col justify-end text-left"
                >
                  <div className="mb-1 text-xs">
                    <span className="font-semibold">{s.users.toLocaleString()}</span> <span className="text-muted">{pct(s.conversion_overall)}</span>
                  </div>
                  <div className="relative flex-1">
                    <div className="absolute inset-x-0 bottom-0 rounded-t bg-danger/15" style={{ height: `${(result.steps[Math.max(0, i - 1)].users / top) * 100}%` }} />
                    <div
                      className={cn('absolute inset-x-0 bottom-0 rounded-t bg-primary transition-opacity', i === focus || i === focus + 1 ? 'opacity-100' : 'opacity-60 group-hover:opacity-80')}
                      style={{ height: `${(s.users / top) * 100}%` }}
                    />
                  </div>
                  <div className="mt-2 truncate text-xs font-medium">
                    {i + 1}. {s.event}
                  </div>
                  <div className="text-[11px] text-muted">{i > 0 ? `${pct(s.conversion_from_previous)} from previous` : 'entered'}</div>
                </button>
              ))}
            </div>
          </Card>
          <DropOffs result={result} focus={focus} setFocus={setFocus} />
        </div>
      )}
    </>
  );
}

function DropOffs({ result, focus, setFocus }: { result: FunnelResult; focus: number; setFocus: (i: number) => void }) {
  const st = result.steps[focus];
  const next = result.steps[focus + 1];
  if (!st?.dropped || !next) return null;
  return (
    <Card className="border-danger/30" data-testid="dropoff-panel">
      <CardHeader
        title={
          <span>
            Why users dropped between <span className="text-primary">{st.event}</span> and <span className="text-primary">{next.event}</span>
          </span>
        }
        description="Backend health in the sessions of users who stopped here, next to those who continued."
        actions={
          <Select aria-label="Drop-off step" value={focus} onChange={(e) => setFocus(Number(e.target.value))} className="h-8 text-xs">
            {result.steps.slice(0, -1).map((s, i) => (
              <option key={i} value={i}>
                Step {i + 1} → {i + 2}
              </option>
            ))}
          </Select>
        }
      />
      <div className="grid grid-cols-2 gap-px bg-border">
        <Cohort title="Dropped" tone="danger" c={st.dropped} />
        <Cohort title="Continued" tone="ok" c={st.continued} />
      </div>
      <div className="p-4">
        <div className="mb-2 text-xs font-medium uppercase tracking-wide text-muted">Top errors for users who dropped</div>
        {st.dropped.top_errors.length === 0 ? (
          <div className="text-sm text-muted">No errors in these sessions.</div>
        ) : (
          <ul className="divide-y divide-border" data-testid="dropoff-errors">
            {st.dropped.top_errors.map((e) => (
              <li key={e.issue_id} className="flex items-center justify-between gap-3 py-2 text-sm">
                <div className="min-w-0">
                  <Link href={`/issues/${e.issue_id}`} className="block truncate font-medium hover:text-primary">
                    {e.title}
                  </Link>
                  <div className="text-xs text-muted">{e.service_name || 'browser'}</div>
                </div>
                <div className="shrink-0 text-right text-xs">
                  <div>
                    <span className="font-semibold text-red-300">{e.users}</span> of {st.dropped!.users} users
                  </div>
                  <div className="text-muted">{e.count} events</div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  );
}

function Cohort({ title, tone, c }: { title: string; tone: 'danger' | 'ok'; c?: CohortHealth }) {
  const errRate = c && c.sessions ? c.error_sessions / c.sessions : 0;
  return (
    <div className="bg-card p-4" data-testid={`cohort-${title.toLowerCase()}`}>
      <div className="mb-3 flex items-center justify-between">
        <Badge tone={tone}>{title}</Badge>
        <span className="text-xs text-muted">{c?.users ?? 0} users</span>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <div className="text-xs text-muted">Sessions with errors</div>
          <div className={cn('text-2xl font-semibold', tone === 'danger' && errRate > 0 && 'text-red-300')}>{pct(errRate)}</div>
        </div>
        <div>
          <div className="text-xs text-muted">Backend p95</div>
          <div className={cn('text-2xl font-semibold', tone === 'danger' && (c?.p95_latency_ms ?? 0) > 1000 && 'text-amber-300')}>
            {c?.p95_latency_ms ? `${Math.round(c.p95_latency_ms).toLocaleString()} ms` : '—'}
          </div>
        </div>
      </div>
    </div>
  );
}
