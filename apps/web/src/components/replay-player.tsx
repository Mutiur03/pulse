'use client';
import '@rrweb/replay/dist/style.css';
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import type { Replayer as ReplayerType } from '@rrweb/replay';
import { duration } from '@/lib/format';
import { cn } from '@/lib/cn';

export interface Marker {
  /** Epoch ms. */
  at: number;
  kind: 'error' | 'request' | 'pageview';
  label: string;
}

export interface PlayerHandle {
  seek(at: number): void;
}

const markerColor: Record<Marker['kind'], string> = {
  error: 'bg-danger',
  request: 'bg-browser',
  pageview: 'bg-muted',
};

/** rrweb replayer with a timeline that shows error, request and page markers. */
export const ReplayPlayer = forwardRef<PlayerHandle, { events: unknown[]; markers: Marker[]; start?: number; onTime?: (at: number) => void }>(
  function ReplayPlayer({ events, markers, start, onTime }, ref) {
    const box = useRef<HTMLDivElement>(null);
    const stage = useRef<HTMLDivElement>(null);
    const rep = useRef<ReplayerType | undefined>(undefined);
    const [meta, setMeta] = useState<{ startTime: number; totalTime: number }>();
    const [offset, setOffset] = useState(0);
    const [playing, setPlaying] = useState(false);
    const [speed, setSpeed] = useState(1);
    const [error, setError] = useState<string>();

    useEffect(() => {
      let alive = true;
      let timer: ReturnType<typeof setInterval> | undefined;
      void (async () => {
        if (!stage.current || events.length < 2) {
          setError('This recording has no playable events yet.');
          return;
        }
        const { Replayer } = await import('@rrweb/replay');
        if (!alive || !stage.current) return;
        stage.current.innerHTML = '';
        const r = new Replayer(events as ConstructorParameters<typeof Replayer>[0], {
          root: stage.current,
          skipInactive: true,
          showWarning: false,
          mouseTail: false,
        });
        rep.current = r;
        const fit = (w: number, h: number) => {
          const el = box.current;
          if (!el || !w || !h) return;
          const scale = Math.min(el.clientWidth / w, el.clientHeight / h, 1);
          r.wrapper.style.transform = `scale(${scale})`;
          r.wrapper.style.transformOrigin = 'top left';
          r.wrapper.style.width = `${w}px`;
          r.wrapper.style.height = `${h}px`;
          r.wrapper.style.left = `${(el.clientWidth - w * scale) / 2}px`;
          r.wrapper.style.position = 'absolute';
        };
        r.on('resize', (d) => {
          const { width, height } = d as { width: number; height: number };
          fit(width, height);
        });
        r.on('finish', () => setPlaying(false));
        const m = r.getMetaData();
        setMeta(m);
        const first = start ? Math.min(Math.max(0, start - m.startTime - 1500), m.totalTime) : 0;
        r.pause(first);
        setOffset(first);
        timer = setInterval(() => {
          const t = r.getCurrentTime();
          setOffset(t);
          onTime?.(m.startTime + t);
        }, 200);
      })().catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)));
      return () => {
        alive = false;
        clearInterval(timer);
        rep.current?.destroy();
        rep.current = undefined;
      };
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [events]);

    const seek = (at: number) => {
      const r = rep.current;
      if (!r || !meta) return;
      const t = Math.min(Math.max(0, at - meta.startTime), meta.totalTime);
      if (playing) r.play(t);
      else r.pause(t);
      setOffset(t);
      onTime?.(meta.startTime + t);
    };
    useImperativeHandle(ref, () => ({ seek }));

    const toggle = () => {
      const r = rep.current;
      if (!r || !meta) return;
      if (playing) {
        r.pause();
        setPlaying(false);
      } else {
        r.play(offset >= meta.totalTime ? 0 : offset);
        setPlaying(true);
      }
    };
    const total = meta?.totalTime ?? 0;

    return (
      <div className="flex flex-col gap-2">
        <div ref={box} className="relative h-[480px] overflow-hidden rounded-md border border-border bg-raised" data-testid="replay-stage">
          <div ref={stage} className="replay-root" />
          {error && <div className="absolute inset-0 grid place-items-center text-sm text-muted">{error}</div>}
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={toggle}
            disabled={!meta}
            className="h-8 w-16 rounded-md bg-primary text-xs font-medium text-primary-fg disabled:opacity-50"
            data-testid="replay-play"
          >
            {playing ? 'Pause' : 'Play'}
          </button>
          <span className="w-24 font-mono text-xs text-fg-2" data-testid="replay-time">
            {duration(offset)} / {duration(total)}
          </span>
          <div
            className="relative h-6 flex-1 select-none"
            data-testid="replay-timeline"
            onClick={(e) => {
              if (!meta) return;
              const rect = e.currentTarget.getBoundingClientRect();
              seek(meta.startTime + ((e.clientX - rect.left) / rect.width) * total);
            }}
          >
            <div className="absolute top-1/2 h-1 w-full -translate-y-1/2 rounded bg-accent" />
            <div className="absolute top-1/2 h-1 -translate-y-1/2 rounded bg-primary" style={{ width: `${total ? (offset / total) * 100 : 0}%` }} />
            {meta &&
              markers.map((m, i) => (
                <span
                  key={i}
                  title={m.label}
                  data-testid={`marker-${m.kind}`}
                  className={cn('absolute top-0 h-6 w-1 -translate-x-1/2 rounded', markerColor[m.kind], m.kind === 'pageview' && 'h-3 opacity-60')}
                  style={{ left: `${Math.min(100, Math.max(0, ((m.at - meta.startTime) / Math.max(total, 1)) * 100))}%` }}
                />
              ))}
          </div>
          <select
            aria-label="Speed"
            value={speed}
            onChange={(e) => {
              const v = Number(e.target.value);
              setSpeed(v);
              rep.current?.setConfig({ speed: v });
            }}
            className="h-8 rounded-md border border-border bg-background px-1 text-xs"
          >
            {[1, 2, 4, 8].map((s) => (
              <option key={s} value={s}>
                {s}x
              </option>
            ))}
          </select>
        </div>
      </div>
    );
  },
);
