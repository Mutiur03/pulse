'use client';
import type { PropertyFilter, TrendInterval, TrendQuery, TrendSeriesInput } from '@pulse/api-client';
import { Button, Input, Select } from './ui';
import type { TrendDisplay } from './charts';

export type SavedTrend = TrendQuery & { display?: TrendDisplay };

const maths: [NonNullable<TrendSeriesInput['math']>, string][] = [
  ['total', 'Total count'],
  ['unique_users', 'Unique users'],
  ['unique_sessions', 'Unique sessions'],
  ['sum', 'Sum of property'],
  ['avg', 'Average of property'],
  ['min', 'Minimum of property'],
  ['max', 'Maximum of property'],
  ['p95', 'p95 of property'],
];
const needsProperty = new Set(['sum', 'avg', 'min', 'max', 'p95']);
const ops: [PropertyFilter['op'], string][] = [
  ['eq', '='],
  ['neq', '≠'],
  ['contains', 'contains'],
  ['not_contains', 'does not contain'],
  ['gt', '>'],
  ['lt', '<'],
  ['is_set', 'is set'],
  ['is_not_set', 'is not set'],
];
const intervals: TrendInterval[] = ['hour', 'day', 'week', 'month'];
const displays: [TrendDisplay, string][] = [
  ['line', 'Line chart'],
  ['bar', 'Bar (totals)'],
  ['number', 'Number'],
  ['table', 'Table'],
];
/** Suggested properties: event columns and what the browser SDK sends. */
export const knownProperties = ['$pathname', '$current_url', '$referrer', '$title', '$metric', '$value', '$rating', '$event_type', '$tag', 'distinct_id', 'session_id', 'event'];

export const letter = (i: number) => String.fromCharCode(65 + i);

function Filters({ value, onChange, label }: { value: PropertyFilter[]; onChange: (f: PropertyFilter[]) => void; label: string }) {
  const set = (i: number, patch: Partial<PropertyFilter>) => onChange(value.map((f, j) => (j === i ? { ...f, ...patch } : f)));
  return (
    <div className="flex flex-col gap-1.5">
      {value.map((f, i) => (
        <div key={i} className="flex flex-wrap items-center gap-1.5" data-testid="filter-row">
          <span className="w-10 text-right text-[11px] uppercase text-muted">{i === 0 ? 'where' : 'and'}</span>
          <Input
            aria-label={`${label} filter ${i + 1} property`}
            list="known-properties"
            value={f.property}
            onChange={(e) => set(i, { property: e.target.value })}
            placeholder="property"
            className="h-8 w-40 text-xs"
          />
          <Select aria-label={`${label} filter ${i + 1} operator`} value={f.op} onChange={(e) => set(i, { op: e.target.value as PropertyFilter['op'] })} className="h-8 text-xs">
            {ops.map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </Select>
          {f.op !== 'is_set' && f.op !== 'is_not_set' && (
            <Input
              aria-label={`${label} filter ${i + 1} value`}
              value={f.value ?? ''}
              onChange={(e) => set(i, { value: e.target.value })}
              placeholder="value"
              className="h-8 w-36 text-xs"
            />
          )}
          <button aria-label={`Remove ${label} filter ${i + 1}`} onClick={() => onChange(value.filter((_, j) => j !== i))} className="px-1 text-muted hover:text-fg">
            ×
          </button>
        </div>
      ))}
      <button className="w-fit text-xs text-primary hover:underline" onClick={() => onChange([...value, { property: '$pathname', op: 'eq', value: '' }])}>
        + Filter
      </button>
    </div>
  );
}

/** Edits a trend query: series with math and filters, global filters, breakdown, interval, formula and display. */
export function TrendBuilder({ value, onChange, events }: { value: SavedTrend; onChange: (q: SavedTrend) => void; events: string[] }) {
  const series = value.series;
  const setSeries = (i: number, patch: Partial<TrendSeriesInput>) => onChange({ ...value, series: series.map((s, j) => (j === i ? { ...s, ...patch } : s)) });
  const options = Array.from(new Set([...events, ...series.map((s) => s.event ?? '').filter(Boolean)]));
  return (
    <div className="flex flex-col gap-4" data-testid="trend-builder">
      <datalist id="known-properties">
        {knownProperties.map((p) => (
          <option key={p} value={p} />
        ))}
      </datalist>
      <div className="flex flex-col gap-3">
        {series.map((s, i) => (
          <div key={i} className="rounded-md bg-raised/70 px-3 py-2.5" data-testid="series-row">
            <div className="flex flex-wrap items-center gap-2">
              <span className="grid h-6 w-6 place-items-center rounded bg-primary/20 text-xs font-semibold text-primary">{letter(i)}</span>
              <Select aria-label={`Series ${letter(i)} event`} value={s.event ?? ''} onChange={(e) => setSeries(i, { event: e.target.value })} className="h-8 w-52 text-xs">
                <option value="">All events</option>
                {options.map((o) => (
                  <option key={o} value={o}>
                    {o}
                  </option>
                ))}
              </Select>
              <Select
                aria-label={`Series ${letter(i)} math`}
                value={s.math ?? 'total'}
                onChange={(e) => setSeries(i, { math: e.target.value as TrendSeriesInput['math'] })}
                className="h-8 text-xs"
              >
                {maths.map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
              {needsProperty.has(s.math ?? '') && (
                <Input
                  aria-label={`Series ${letter(i)} property`}
                  list="known-properties"
                  value={s.property ?? ''}
                  onChange={(e) => setSeries(i, { property: e.target.value })}
                  placeholder="numeric property"
                  className="h-8 w-40 text-xs"
                />
              )}
              {series.length > 1 && (
                <button aria-label={`Remove series ${letter(i)}`} onClick={() => onChange({ ...value, series: series.filter((_, j) => j !== i) })} className="ml-auto px-1 text-muted hover:text-fg">
                  ×
                </button>
              )}
            </div>
            <div className="mt-2">
              <Filters label={`Series ${letter(i)}`} value={s.filters ?? []} onChange={(f) => setSeries(i, { filters: f })} />
            </div>
          </div>
        ))}
        {series.length < 10 && (
          <Button variant="outline" size="sm" className="w-fit" onClick={() => onChange({ ...value, series: [...series, { event: options[0] ?? '', math: 'total' }] })}>
            + Series
          </Button>
        )}
      </div>
      <div className="grid gap-3 border-t border-border pt-3 md:grid-cols-2">
        <div>
          <div className="mb-1 text-xs text-muted">Filters on every series</div>
          <Filters label="Global" value={value.filters ?? []} onChange={(f) => onChange({ ...value, filters: f })} />
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-xs text-muted">
            Breakdown by
            <Input
              aria-label="Breakdown"
              list="known-properties"
              value={value.breakdown ?? ''}
              onChange={(e) => onChange({ ...value, breakdown: e.target.value })}
              placeholder="none"
              className="h-8 w-40 text-xs"
            />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted">
            Interval
            <Select aria-label="Interval" value={value.interval ?? 'day'} onChange={(e) => onChange({ ...value, interval: e.target.value as TrendInterval })} className="h-8 text-xs">
              {intervals.map((v) => (
                <option key={v}>{v}</option>
              ))}
            </Select>
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted">
            Formula
            <Input aria-label="Formula" value={value.formula ?? ''} onChange={(e) => onChange({ ...value, formula: e.target.value })} placeholder="e.g. B / A * 100" className="h-8 w-40 text-xs" />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted">
            Display
            <Select aria-label="Display" value={value.display ?? 'line'} onChange={(e) => onChange({ ...value, display: e.target.value as TrendDisplay })} className="h-8 text-xs">
              {displays.map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          </label>
        </div>
      </div>
    </div>
  );
}

/** The request body for a saved trend over a dashboard time range. */
export function trendRequest(q: SavedTrend, from: string, to?: string): TrendQuery {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { display, ...rest } = q;
  const clean = (fs?: PropertyFilter[]) => fs?.filter((f) => f.property.trim());
  return {
    ...rest,
    breakdown: rest.breakdown?.trim() || undefined,
    formula: rest.formula?.trim() || undefined,
    filters: clean(rest.filters),
    series: rest.series.map((s) => ({ ...s, filters: clean(s.filters) })),
    from,
    to,
  };
}
