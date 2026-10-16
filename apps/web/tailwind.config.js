/** @type {import('tailwindcss').Config} */
// Every color is a design token defined as RGB channels in src/app/globals.css,
// so the dark and light themes swap by changing the variables only.
const token = (name) => `rgb(var(--${name}) / <alpha-value>)`;

module.exports = {
  darkMode: 'class',
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        background: token('bg'),
        card: token('surface'),
        surface: token('surface'),
        raised: token('surface-2'),
        border: token('border'),
        strong: token('border-strong'),
        accent: token('accent'),
        fg: token('fg'),
        'fg-2': token('fg-2'),
        muted: token('muted'),
        subtle: token('subtle'),
        primary: token('primary'),
        'primary-fg': token('primary-fg'),
        browser: token('browser'),
        backend: token('backend'),
        database: token('database'),
        danger: token('danger'),
        ok: token('ok'),
        warn: token('warn'),
        'danger-fg': token('danger-fg'),
        'ok-fg': token('ok-fg'),
        'warn-fg': token('warn-fg'),
        'browser-fg': token('browser-fg'),
        'backend-fg': token('backend-fg'),
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['ui-monospace', 'SFMono-Regular', 'JetBrains Mono', 'Menlo', 'monospace'],
      },
      fontSize: { '2xs': ['0.6875rem', '1rem'] },
      borderRadius: { DEFAULT: '0.25rem', md: '0.375rem', lg: '0.5rem' },
      boxShadow: { pop: '0 8px 24px -8px rgb(0 0 0 / 0.35), 0 0 0 1px rgb(var(--border))' },
    },
  },
  plugins: [],
};
