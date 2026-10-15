import type { AlertComparison, AlertMetric, AlertState } from '@pulse/api-client';
import type { Tone } from '@/components/ui';

export const metrics: { value: AlertMetric; label: string; help: string }[] = [
  { value: 'error_count', label: 'Error count', help: 'Errors in the window' },
  { value: 'error_rate', label: 'Error rate', help: 'Errors per minute' },
  { value: 'failure_rate', label: 'Failure rate', help: 'Percent of server requests that failed' },
  { value: 'latency_p95', label: 'Latency p95', help: 'Server span duration, ms' },
  { value: 'latency_p99', label: 'Latency p99', help: 'Server span duration, ms' },
  { value: 'throughput', label: 'Throughput', help: 'Server requests per minute; use "below" for drops' },
  { value: 'event_count', label: 'Event count', help: 'Product events with this name' },
  { value: 'new_issue', label: 'New issue', help: 'Fires on every new issue' },
  { value: 'issue_regression', label: 'Issue regression', help: 'Fires when a resolved issue comes back' },
];

export const comparisons: { value: AlertComparison; label: string }[] = [
  { value: 'gt', label: 'above' },
  { value: 'gte', label: 'at or above' },
  { value: 'lt', label: 'below' },
  { value: 'lte', label: 'at or below' },
];

export const windows = [60, 300, 600, 900, 1800, 3600, 3 * 3600, 6 * 3600, 12 * 3600, 86400];
export const intervals = [10, 30, 60, 300, 900, 3600];

export function eventDriven(m: AlertMetric): boolean {
  return m === 'new_issue' || m === 'issue_regression';
}

export function span(sec: number): string {
  if (sec % 3600 === 0) return `${sec / 3600}h`;
  if (sec % 60 === 0) return `${sec / 60}m`;
  return `${sec}s`;
}

export const stateTone: Record<AlertState, Tone> = { alerting: 'danger', ok: 'ok', no_data: 'warn' };
export const stateLabel: Record<AlertState, string> = { alerting: 'Alerting', ok: 'OK', no_data: 'No data' };

export function value(v: number | undefined, unit: string): string {
  if (v === undefined || v === null) return '—';
  return `${Math.round(v * 100) / 100}${unit}`;
}
