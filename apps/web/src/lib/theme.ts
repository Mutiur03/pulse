export type Theme = 'system' | 'light' | 'dark';
export const themes: Theme[] = ['system', 'light', 'dark'];

/** The class on <html> for a stored theme; unknown values follow the system. */
export function themeClass(v: string | undefined): Theme {
  return v === 'light' || v === 'dark' ? v : 'system';
}

/** Applies a theme now and remembers it for a year. */
export function applyTheme(t: Theme) {
  const root = document.documentElement;
  root.classList.remove(...themes);
  root.classList.add(t);
  document.cookie = `pulse-theme=${t}; path=/; max-age=31536000; samesite=lax`;
}

export function currentTheme(): Theme {
  const c = document.documentElement.classList;
  return c.contains('light') ? 'light' : c.contains('dark') ? 'dark' : 'system';
}
