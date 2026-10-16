'use client';
import { useEffect, useState } from 'react';
import { applyTheme, currentTheme, themes, type Theme } from '@/lib/theme';
import { cn } from '@/lib/cn';

const icons: Record<Theme, string> = {
  system: 'M3 5h18v11H3zM8 20h8M12 16v4',
  light: 'M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10ZM12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4',
  dark: 'M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z',
};
const labels: Record<Theme, string> = { system: 'System', light: 'Light', dark: 'Dark' };

/** A segmented control for the color theme. */
export function ThemeToggle({ className }: { className?: string }) {
  const [theme, setTheme] = useState<Theme>('system');
  useEffect(() => setTheme(currentTheme()), []);
  return (
    <div role="radiogroup" aria-label="Color theme" className={cn('inline-flex rounded-md border border-border bg-background p-0.5', className)}>
      {themes.map((t) => (
        <button
          key={t}
          type="button"
          role="radio"
          aria-checked={theme === t}
          aria-label={`${labels[t]} theme`}
          title={`${labels[t]} theme`}
          onClick={() => {
            applyTheme(t);
            setTheme(t);
          }}
          className={cn(
            'grid h-6 w-7 place-items-center rounded text-subtle transition-colors hover:text-fg',
            theme === t && 'bg-accent text-fg shadow-sm',
          )}
        >
          <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
            <path d={icons[t]} />
          </svg>
        </button>
      ))}
    </div>
  );
}
