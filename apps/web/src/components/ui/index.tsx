import { forwardRef, type ButtonHTMLAttributes, type HTMLAttributes, type InputHTMLAttributes, type SelectHTMLAttributes } from 'react';
import { cn } from '@/lib/cn';

type Variant = 'default' | 'outline' | 'ghost' | 'danger';
const variants: Record<Variant, string> = {
  default: 'bg-primary text-primary-fg shadow-sm hover:bg-primary/90',
  outline: 'border border-border bg-surface text-fg shadow-sm hover:border-strong hover:bg-accent',
  ghost: 'text-fg-2 hover:bg-accent hover:text-fg',
  danger: 'border border-danger/40 bg-danger/10 text-danger-fg hover:bg-danger/15',
};

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'sm' | 'md' }>(
  ({ className, variant = 'default', size = 'md', ...p }, ref) => (
    <button
      ref={ref}
      className={cn(
        'inline-flex select-none items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium transition-colors disabled:pointer-events-none disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-1 focus-visible:ring-offset-background',
        size === 'sm' ? 'h-7 px-2.5 text-xs' : 'h-8 px-3 text-[13px]',
        variants[variant],
        className,
      )}
      {...p}
    />
  ),
);
Button.displayName = 'Button';

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...p }, ref) => (
  <input
    ref={ref}
    className={cn(
      'h-8 rounded-md border border-border bg-surface px-2.5 text-[13px] text-fg shadow-sm transition-colors placeholder:text-subtle hover:border-strong focus:border-primary/70 focus:outline-none focus:ring-2 focus:ring-primary/20 disabled:opacity-60',
      className,
    )}
    {...p}
  />
));
Input.displayName = 'Input';

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(({ className, ...p }, ref) => (
  <select
    ref={ref}
    className={cn('h-8 rounded-md border border-border bg-surface px-2 text-[13px] text-fg shadow-sm transition-colors hover:border-strong focus:border-primary/70 focus:outline-none focus:ring-2 focus:ring-primary/20', className)}
    {...p}
  />
));
Select.displayName = 'Select';

export function Card({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('rounded-lg border border-border bg-surface', className)} {...p} />;
}

export function CardHeader({ title, description, actions }: { title: React.ReactNode; description?: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2 border-b border-border px-4 py-2.5">
      <div className="min-w-0">
        <h3 className="text-[13px] font-semibold text-fg">{title}</h3>
        {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
      </div>
      {actions}
    </div>
  );
}

const tones = {
  neutral: 'bg-accent text-fg-2 ring-1 ring-inset ring-border',
  danger: 'bg-danger/10 text-danger-fg ring-1 ring-inset ring-danger/25',
  ok: 'bg-ok/10 text-ok-fg ring-1 ring-inset ring-ok/25',
  warn: 'bg-warn/10 text-warn-fg ring-1 ring-inset ring-warn/25',
  browser: 'bg-browser/10 text-browser-fg ring-1 ring-inset ring-browser/25',
  backend: 'bg-backend/10 text-backend-fg ring-1 ring-inset ring-backend/25',
  database: 'bg-database/10 text-warn-fg ring-1 ring-inset ring-database/25',
};
export type Tone = keyof typeof tones;

export function Badge({ tone = 'neutral', className, ...p }: HTMLAttributes<HTMLSpanElement> & { tone?: Tone }) {
  return <span className={cn('inline-flex h-5 items-center gap-1 whitespace-nowrap rounded px-1.5 text-[11px] font-medium leading-none', tones[tone], className)} {...p} />;
}

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: React.ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-end justify-between gap-3 border-b border-border pb-4">
      <div className="min-w-0">
        <h1 className="text-lg font-semibold tracking-tight text-fg">{title}</h1>
        {description && <p className="mt-0.5 max-w-3xl text-[13px] text-muted">{description}</p>}
      </div>
      {actions}
    </div>
  );
}

/**
 * The empty state of a list or chart: an optional title, an explanation and
 * an optional next step (a link or a button).
 */
export function Empty({
  title,
  children,
  action,
}: {
  title?: string;
  children?: React.ReactNode;
  action?: { label: string; href?: string; onClick?: () => void };
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-12 text-center" data-testid="empty-state">
      <div className="flex h-9 w-9 items-center justify-center rounded-md border border-dashed border-strong text-subtle" aria-hidden>
        <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M3 7h18M3 12h18M3 17h10" />
        </svg>
      </div>
      {title && <div className="text-[13px] font-medium text-fg">{title}</div>}
      {children && <div className="max-w-md text-[13px] text-muted">{children}</div>}
      {action &&
        (action.href ? (
          <a href={action.href} className="mt-1 inline-flex h-8 items-center rounded-md bg-primary px-3 text-xs font-medium text-primary-fg hover:bg-primary/90">
            {action.label}
          </a>
        ) : (
          <Button size="sm" onClick={action.onClick} className="mt-1">
            {action.label}
          </Button>
        ))}
    </div>
  );
}

/** Placeholder rows while a list loads for the first time. */
export function Loading({ rows = 5, label = 'Loading' }: { rows?: number; label?: string }) {
  return (
    <div className="divide-y divide-border" role="status" aria-label={label} data-testid="loading">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-4 px-4 py-3">
          <div className="h-3 w-1/3 animate-pulse rounded bg-accent" />
          <div className="h-3 w-1/5 animate-pulse rounded bg-accent/70" />
          <div className="ml-auto h-3 w-16 animate-pulse rounded bg-accent/70" />
        </div>
      ))}
    </div>
  );
}

// Titles for errors of queries stopped by the API's cost limits; the
// messages come from the query API.
const limitTitles: [string, string][] = [
  ['This query took too long', 'Query timed out'],
  ['This query would read too much data', 'Query too large'],
  ['Too many queries are running', 'Too many queries at once'],
];

/** The title for an error from a query stopped by a cost limit, if it is one. */
function limitTitle(error: string): string | undefined {
  return limitTitles.find(([prefix]) => error.startsWith(prefix))?.[1];
}

export function ErrorNote({ error, onRetry }: { error?: string; onRetry?: () => void }) {
  if (!error) return null;
  const title = limitTitle(error);
  const tone = title ? 'border-warn/30 bg-warn/10 text-warn-fg' : 'border-danger/30 bg-danger/10 text-danger-fg';
  return (
    <div role="alert" className={`mb-3 flex items-start justify-between gap-3 rounded-md border px-3 py-2 text-[13px] ${tone}`}>
      <div>
        {title && <div className="font-medium">{title}</div>}
        <div>{error}</div>
      </div>
      {onRetry && (
        <button type="button" onClick={onRetry} className="shrink-0 rounded border border-strong px-2 py-0.5 text-xs hover:bg-accent">
          Retry
        </button>
      )}
    </div>
  );
}

export function Spinner() {
  return <div className="h-4 w-4 animate-spin rounded-full border-2 border-strong border-t-primary" />;
}

export function Th({ className, ...p }: HTMLAttributes<HTMLTableCellElement>) {
  return <th className={cn('h-8 whitespace-nowrap px-3 text-left text-[11px] font-medium uppercase tracking-wider text-subtle first:pl-4 last:pr-4', className)} {...p} />;
}

export function Td({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn('px-3 py-2 align-middle first:pl-4 last:pr-4', className)} {...p} />;
}

/** The filter and control row above a list or chart; flat, not a card. */
export function Toolbar({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('mb-3 flex flex-wrap items-center gap-2', className)} {...p} />;
}
