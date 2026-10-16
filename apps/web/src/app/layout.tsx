import type { Metadata, Viewport } from 'next';
import { cookies, headers } from 'next/headers';
import { themeClass } from '@/lib/theme';
import './globals.css';

export const metadata: Metadata = { title: 'Pulse', description: 'Product analytics, traces and errors in one place' };
export const viewport: Viewport = { width: 'device-width', initialScale: 1 };

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // Reading the request headers renders every page per request, so Next.js
  // can put the CSP nonce from src/middleware.ts on its scripts.
  await headers();
  // The theme lives in a cookie so the server renders the right class and the
  // page never flashes the other theme (no inline script is needed).
  const theme = themeClass((await cookies()).get('pulse-theme')?.value);
  return (
    <html lang="en" className={theme} suppressHydrationWarning>
      <body>{children}</body>
    </html>
  );
}
