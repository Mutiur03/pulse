import type { Tone } from '@/components/ui';

export const levelOrder = ['FATAL', 'ERROR', 'WARN', 'INFO', 'DEBUG', 'TRACE', 'NONE'];

export const levelColor: Record<string, string> = {
  FATAL: '#f43f5e',
  ERROR: '#ef4444',
  WARN: '#f59e0b',
  INFO: '#38bdf8',
  DEBUG: '#a1a1aa',
  TRACE: '#71717a',
  NONE: '#52525b',
};

export function levelTone(level: string): Tone {
  switch (level) {
    case 'FATAL':
    case 'ERROR':
      return 'danger';
    case 'WARN':
    case 'WARNING':
      return 'warn';
    case 'INFO':
      return 'browser';
  }
  return 'neutral';
}

/** Adds a term to a logs search, replacing an existing term on the same field. */
export function withTerm(q: string, field: string, value: string): string {
  const v = /\s/.test(value) ? `"${value}"` : value;
  const kept = q
    .split(/\s+/)
    .filter((t) => t && !t.toLowerCase().startsWith(`${field}:`) && !t.toLowerCase().startsWith(`-${field}:`));
  return [...kept, `${field}:${v}`].join(' ');
}
