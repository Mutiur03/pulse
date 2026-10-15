export function ms(ns: number): string {
  const v = ns / 1e6;
  if (v >= 1000) return `${(v / 1000).toFixed(v >= 10000 ? 1 : 2)} s`;
  if (v >= 10) return `${Math.round(v)} ms`;
  return `${v.toFixed(v >= 1 ? 1 : 2)} ms`;
}

export function ago(iso: string): string {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return `${Math.floor(s)}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export function time(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

export function pct(x: number): string {
  return `${(x * 100).toFixed(x > 0 && x < 0.1 ? 1 : 0)}%`;
}

export const ranges: Record<string, number> = { '15m': 15 * 60e3, '1h': 3600e3, '24h': 86400e3, '7d': 7 * 86400e3 };

export function fromRange(r: string | undefined, def = '24h'): string {
  return new Date(Date.now() - (ranges[r ?? def] ?? ranges[def])).toISOString();
}
