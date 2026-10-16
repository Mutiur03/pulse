import { NextRequest, NextResponse } from 'next/server';

// Pages that work without a session.
const publicPage = /^\/(login|signup|reset-password|verify-email|invite)/;

/**
 * A strict Content-Security-Policy with a per-request nonce. Next.js reads
 * the nonce from the request's CSP header and puts it on its own scripts;
 * 'strict-dynamic' lets those load the rest of the app. Styles allow inline
 * because React style attributes and the replay player need them.
 */
export function contentSecurityPolicy(nonce: string, dev: boolean): string {
  return [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${dev ? " 'unsafe-eval'" : ''}`,
    "style-src 'self' 'unsafe-inline'",
    // Session replays show images and fonts from the recorded sites.
    "img-src 'self' data: blob: https: http:",
    "font-src 'self' data: https:",
    `connect-src 'self'${dev ? ' ws:' : ''}`,
    "media-src 'self' blob: data:",
    "frame-src 'self' blob:",
    "worker-src 'self' blob:",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join('; ');
}

function hsts(): string | undefined {
  // HSTS_MAX_AGE=0 turns it off; browsers ignore it over plain HTTP anyway.
  const age = Number(process.env.HSTS_MAX_AGE ?? 31536000);
  return Number.isFinite(age) && age > 0 ? `max-age=${Math.floor(age)}; includeSubDomains` : undefined;
}

export function middleware(req: NextRequest) {
  const { pathname, search } = req.nextUrl;
  // Pages need a session cookie; the API validates it. Without one, go to login.
  if (!publicPage.test(pathname) && !req.cookies.get('pulse_session')) {
    const url = req.nextUrl.clone();
    url.pathname = '/login';
    url.search = pathname === '/' ? '' : `?next=${encodeURIComponent(pathname + search)}`;
    return NextResponse.redirect(url);
  }

  const nonce = btoa(crypto.randomUUID());
  const policy = contentSecurityPolicy(nonce, process.env.NODE_ENV === 'development');
  const headers = new Headers(req.headers);
  headers.set('x-nonce', nonce);
  headers.set('content-security-policy', policy);
  const res = NextResponse.next({ request: { headers } });
  res.headers.set('content-security-policy', policy);
  const sts = hsts();
  if (sts) res.headers.set('strict-transport-security', sts);
  return res;
}

export const config = {
  matcher: [
    {
      source: '/((?!api|_next/static|_next/image|favicon.ico).*)',
      // Prefetches do not render HTML that needs a nonce.
      missing: [
        { type: 'header', key: 'next-router-prefetch' },
        { type: 'header', key: 'purpose', value: 'prefetch' },
      ],
    },
  ],
};
