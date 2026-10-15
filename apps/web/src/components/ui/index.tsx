import { forwardRef, type ButtonHTMLAttributes, type HTMLAttributes, type InputHTMLAttributes, type SelectHTMLAttributes } from 'react';
import { cn } from '@/lib/cn';

type Variant = 'default' | 'outline' | 'ghost' | 'danger';
const variants: Record<Variant, string> = {
  default: 'bg-primary text-white hover:bg-primary/90',
  outline: 'border border-border bg-transparent hover:bg-accent',
  ghost: 'hover:bg-accent',
  danger: 'bg-danger/90 text-white hover:bg-danger',
};

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'sm' | 'md' }>(
  ({ className, variant = 'default', size = 'md', ...p }, ref) => (
    <button
      ref={ref}
      className={cn(
        'inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary',
        size === 'sm' ? 'h-7 px-2.5 text-xs' : 'h-9 px-4 text-sm',
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
      'h-9 rounded-md border border-border bg-background px-3 text-sm placeholder:text-zinc-500 focus:outline-none focus:ring-2 focus:ring-primary/60',
      className,
    )}
    {...p}
  />
));
Input.displayName = 'Input';

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(({ className, ...p }, ref) => (
  <select
    ref={ref}
    className={cn('h-9 rounded-md border border-border bg-background px-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary/60', className)}
    {...p}
  />
));
Select.displayName = 'Select';

export function Card({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('rounded-lg border border-border bg-card', className)} {...p} />;
}

export function CardHeader({ title, description, actions }: { title: React.ReactNode; description?: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-border px-4 py-3">
      <div>
        <h3 className="text-sm font-semibold">{title}</h3>
        {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
      </div>
      {actions}
    </div>
  );
}

const tones = {
  neutral: 'bg-zinc-800 text-zinc-300',
  danger: 'bg-danger/15 text-red-300 ring-1 ring-inset ring-danger/30',
  ok: 'bg-ok/15 text-green-300 ring-1 ring-inset ring-ok/30',
  warn: 'bg-database/15 text-amber-300 ring-1 ring-inset ring-database/30',
  browser: 'bg-browser/15 text-sky-300 ring-1 ring-inset ring-browser/30',
  backend: 'bg-backend/15 text-violet-300 ring-1 ring-inset ring-backend/30',
  database: 'bg-database/15 text-amber-300 ring-1 ring-inset ring-database/30',
};
export type Tone = keyof typeof tones;

export function Badge({ tone = 'neutral', className, ...p }: HTMLAttributes<HTMLSpanElement> & { tone?: Tone }) {
  return <span className={cn('inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium', tones[tone], className)} {...p} />;
}

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: React.ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </div>
      {actions}
    </div>
  );
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="px-4 py-12 text-center text-sm text-muted">{children}</div>;
}

export function ErrorNote({ error }: { error?: string }) {
  if (!error) return null;
  return <div className="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-red-300">{error}</div>;
}

export function Spinner() {
  return <div className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-600 border-t-primary" />;
}

export function Th({ className, ...p }: HTMLAttributes<HTMLTableCellElement>) {
  return <th className={cn('px-4 py-2 text-left text-xs font-medium uppercase tracking-wide text-muted', className)} {...p} />;
}

export function Td({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn('px-4 py-2.5 align-middle', className)} {...p} />;
}
