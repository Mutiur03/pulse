/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: 'class',
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        border: 'hsl(240 4% 18%)',
        background: 'hsl(240 10% 5%)',
        card: 'hsl(240 8% 8%)',
        muted: 'hsl(240 5% 65%)',
        accent: 'hsl(240 5% 14%)',
        primary: 'hsl(262 83% 66%)',
        browser: 'hsl(199 89% 56%)',
        backend: 'hsl(262 83% 66%)',
        database: 'hsl(38 92% 55%)',
        danger: 'hsl(0 84% 62%)',
        ok: 'hsl(142 70% 45%)',
      },
      fontFamily: { mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'] },
    },
  },
  plugins: [],
};
