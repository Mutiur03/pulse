'use client';
import { useState } from 'react';
import type { ErrorOccurrence, StackFrame } from '@pulse/api-client';
import { cn } from '@/lib/cn';
import { Badge, Button } from '@/components/ui';

type Group = { lib: false; frame: StackFrame; index: number } | { lib: true; frames: StackFrame[]; start: number };

/** Runs of library frames fold into one row so app code stands out. */
function group(frames: StackFrame[]): Group[] {
  const out: Group[] = [];
  frames.forEach((f, i) => {
    const last = out[out.length - 1];
    if (f.in_app) out.push({ lib: false, frame: f, index: i });
    else if (last?.lib) last.frames.push(f);
    else out.push({ lib: true, frames: [f], start: i });
  });
  return out;
}

function location(f: StackFrame): string {
  return f.line ? `${f.file}:${f.line}${f.column ? `:${f.column}` : ''}` : f.file;
}

function Frame({ f, open, onToggle }: { f: StackFrame; open: boolean; onToggle: () => void }) {
  const hasCode = !!f.context_line;
  const pre = f.pre_context ?? [];
  const first = f.line - pre.length;
  return (
    <div className="border-b border-border last:border-b-0" data-testid="frame" data-in-app={f.in_app} data-mapped={f.mapped}>
      <div
        role={hasCode ? 'button' : undefined}
        tabIndex={hasCode ? 0 : undefined}
        onClick={hasCode ? onToggle : undefined}
        onKeyDown={(e) => hasCode && e.key === 'Enter' && onToggle()}
        className={cn('flex items-center gap-2 px-4 py-2 font-mono text-xs', hasCode && 'hover:bg-accent/50')}
      >
        {hasCode && <span className="w-3 text-muted">{open ? '▾' : '▸'}</span>}
        <span className={f.in_app ? 'text-zinc-100' : 'text-muted'} data-testid="frame-location">
          {location(f)}
        </span>
        {f.function && (
          <span className="text-muted">
            in <span className="text-sky-300">{f.function}</span>
          </span>
        )}
        <span className="ml-auto flex gap-1">
          {!f.in_app && <Badge>library</Badge>}
          {f.mapped ? <Badge tone="ok">source map</Badge> : <Badge tone="warn">minified</Badge>}
        </span>
      </div>
      {open && hasCode && (
        <div className="overflow-auto bg-zinc-950/60 py-2 font-mono text-xs leading-relaxed" data-testid="frame-code">
          {[...pre, f.context_line!, ...(f.post_context ?? [])].map((line, i) => {
            const n = first + i;
            const hit = n === f.line;
            return (
              <div key={i} className={cn('flex whitespace-pre', hit && 'bg-danger/15 text-red-200')} data-testid={hit ? 'context-line' : undefined}>
                <span className="w-12 shrink-0 select-none pr-3 text-right text-muted">{n}</span>
                <span className={hit ? '' : 'text-zinc-400'}>{line}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

export function StackTrace({ ev, onReprocess, reprocessing }: { ev?: ErrorOccurrence; onReprocess?: () => void; reprocessing?: boolean }) {
  const frames = ev?.frames ?? [];
  const mapped = ev?.mapped_frames ?? 0;
  const [raw, setRaw] = useState(false);
  const [openLibs, setOpenLibs] = useState<Set<number>>(new Set());
  const firstApp = frames.findIndex((f) => f.in_app && f.context_line);
  // Only the first app frame with code starts expanded.
  const [flipped, setFlipped] = useState<Set<number>>(new Set());
  const isOpen = (i: number) => (i === firstApp) !== flipped.has(i);
  const toggle = (i: number) =>
    setFlipped((s) => {
      const n = new Set(s);
      if (n.has(i)) n.delete(i);
      else n.add(i);
      return n;
    });

  if (!ev) return <pre className="p-4 font-mono text-xs text-muted" data-testid="stack">No stack trace captured.</pre>;
  if (frames.length === 0) {
    return (
      <pre className="max-h-[480px] overflow-auto whitespace-pre-wrap p-4 font-mono text-xs leading-relaxed text-zinc-300" data-testid="stack">
        {ev.stack || 'No stack trace captured.'}
      </pre>
    );
  }
  return (
    <div data-testid="stack">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2 text-xs">
        <div className="flex overflow-hidden rounded-md border border-border" role="group" aria-label="Stack view">
          <button
            type="button"
            onClick={() => setRaw(false)}
            className={cn('px-2 py-1', !raw ? 'bg-accent text-zinc-100' : 'text-muted hover:text-zinc-100')}
            data-testid="stack-original"
          >
            Original
          </button>
          <button
            type="button"
            onClick={() => setRaw(true)}
            className={cn('border-l border-border px-2 py-1', raw ? 'bg-accent text-zinc-100' : 'text-muted hover:text-zinc-100')}
            data-testid="stack-raw"
          >
            Minified
          </button>
        </div>
        <span className="text-muted" data-testid="mapped-count">
          {mapped ? `${mapped} of ${frames.length} frames resolved with source maps` : 'No source maps matched; showing minified locations'}
          {ev.release ? ` · release ${ev.release}` : ''}
        </span>
        {onReprocess && (
          <Button size="sm" variant="ghost" className="ml-auto" onClick={onReprocess} disabled={reprocessing} data-testid="reprocess">
            {reprocessing ? 'Reprocessing…' : 'Reprocess'}
          </Button>
        )}
      </div>
      {raw ? (
        <pre className="max-h-[560px] overflow-auto whitespace-pre-wrap p-4 font-mono text-xs leading-relaxed text-zinc-300" data-testid="stack-minified">
          {ev.stack}
        </pre>
      ) : (
        <div className="max-h-[560px] overflow-auto">
          {group(frames).map((g) =>
            g.lib ? (
              openLibs.has(g.start) ? (
                g.frames.map((f, j) => <Frame key={g.start + j} f={f} open={isOpen(g.start + j)} onToggle={() => toggle(g.start + j)} />)
              ) : (
                <button
                  key={g.start}
                  type="button"
                  onClick={() => setOpenLibs((s) => new Set(s).add(g.start))}
                  className="block w-full border-b border-border px-4 py-1.5 text-left text-xs text-muted hover:bg-accent/50 hover:text-zinc-100"
                  data-testid="library-frames"
                >
                  {g.frames.length} library {g.frames.length === 1 ? 'frame' : 'frames'} hidden · show
                </button>
              )
            ) : (
              <Frame key={g.index} f={g.frame} open={isOpen(g.index)} onToggle={() => toggle(g.index)} />
            ),
          )}
        </div>
      )}
    </div>
  );
}
