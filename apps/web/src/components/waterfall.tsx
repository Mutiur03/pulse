'use client';
import type { SpanNode, Trace } from '@pulse/api-client';
import { cn } from '@/lib/cn';
import { ms } from '@/lib/format';

interface Row {
  span: SpanNode;
  depth: number;
}

export function flatten(trace: Trace): Row[] {
  const out: Row[] = [];
  const walk = (n: SpanNode, d: number) => {
    out.push({ span: n, depth: d });
    n.children.forEach((c) => walk(c, d + 1));
  };
  trace.roots.forEach((r) => walk(r, 0));
  return out;
}

const color = { browser: 'bg-browser', backend: 'bg-backend', database: 'bg-database' } as const;

export function Waterfall({ trace, selected, onSelect }: { trace: Trace; selected?: string; onSelect: (id: string) => void }) {
  const rows = flatten(trace);
  const start = new Date(trace.start_time).getTime();
  const total = Math.max(1, trace.duration_ns / 1e6);
  const pos = (iso: string) => ((new Date(iso).getTime() - start) / total) * 100;
  const ticks = [0, 0.25, 0.5, 0.75, 1];

  return (
    <div className="text-xs" data-testid="waterfall">
      <div className="grid grid-cols-[minmax(240px,34%)_1fr] border-b border-border text-muted">
        <div className="px-3 py-1.5">Span</div>
        <div className="relative mr-3 h-7">
          {ticks.map((t) => (
            <span key={t} className="absolute top-1.5 -translate-x-1/2 whitespace-nowrap font-mono first:translate-x-0 last:-translate-x-full" style={{ left: `${t * 100}%` }}>
              {ms(t * trace.duration_ns)}
            </span>
          ))}
        </div>
      </div>
      {rows.map(({ span: s, depth }) => {
        const left = Math.min(99.5, Math.max(0, pos(s.start_time)));
        const width = Math.max(0.4, Math.min(100 - left, (s.duration_ns / 1e6 / total) * 100));
        const failed = s.status_code === 'error' || s.errors.length > 0;
        return (
          <button
            key={s.span_id}
            onClick={() => onSelect(s.span_id)}
            data-testid="span-row"
            data-source={s.source}
            className={cn(
              'grid w-full grid-cols-[minmax(240px,34%)_1fr] items-center border-b border-border/60 text-left hover:bg-accent/50',
              selected === s.span_id && 'bg-accent',
            )}
          >
            <div className="flex min-w-0 items-center gap-1.5 py-1.5 pr-2" style={{ paddingLeft: 12 + depth * 14 }}>
              <span className={cn('h-2 w-2 shrink-0 rounded-sm', color[s.source])} />
              <span className="truncate">{s.name}</span>
              <span className="shrink-0 text-muted">{s.service_name}</span>
              {failed && <span className="shrink-0 rounded bg-danger/20 px-1 text-[10px] text-danger-fg">error</span>}
            </div>
            <div className="relative mr-3 h-7">
              {ticks.slice(1, -1).map((t) => (
                <div key={t} className="absolute inset-y-0 w-px bg-border/50" style={{ left: `${t * 100}%` }} />
              ))}
              <div
                className={cn('absolute top-2 h-3 rounded-sm', failed ? 'bg-danger' : color[s.source], s.orphan && 'opacity-60')}
                style={{ left: `${left}%`, width: `${width}%` }}
              />
              <span className="absolute top-1.5 font-mono text-[10px] text-fg-2" style={{ left: `calc(${Math.min(left + width, 88)}% + 4px)` }}>
                {ms(s.duration_ns)}
              </span>
              {s.errors.map((e, i) => (
                <Marker key={`e${i}`} at={pos(e.timestamp)} title={`${e.exception_type}: ${e.message}`} kind="error" />
              ))}
              {s.events.map((e, i) => (
                <Marker key={`p${i}`} at={pos(e.timestamp)} title={e.event} kind="event" />
              ))}
              {(s.span_events ?? []).map((e, i) => (
                <Marker key={`s${i}`} at={pos(e.time)} title={e.name} kind="span-event" />
              ))}
            </div>
          </button>
        );
      })}
    </div>
  );
}

function Marker({ at, title, kind }: { at: number; title: string; kind: 'error' | 'event' | 'span-event' }) {
  return (
    <span
      title={title}
      data-testid={`marker-${kind}`}
      className={cn(
        'absolute top-1.5 h-4 w-1 -translate-x-1/2 rounded-full ring-2 ring-background',
        kind === 'error' ? 'bg-danger' : kind === 'event' ? 'bg-ok' : 'bg-subtle',
      )}
      style={{ left: `${Math.min(99.5, Math.max(0.5, at))}%` }}
    />
  );
}

