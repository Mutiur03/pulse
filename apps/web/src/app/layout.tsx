import type { Metadata } from 'next';
import { headers } from 'next/headers';
import './globals.css';

export const metadata: Metadata = { title: 'Pulse', description: 'Product analytics, traces and errors in one place' };

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // Reading the request headers renders every page per request, so Next.js
  // can put the CSP nonce from src/middleware.ts on its scripts.
  await headers();
  return (
    <html lang="en" className="dark">
      <body>{children}</body>
    </html>
  );
}
