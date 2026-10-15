import { NextRequest, NextResponse } from 'next/server';

// Pages need a session cookie; the API validates it. Without one, go to login.
export function middleware(req: NextRequest) {
  if (!req.cookies.get('pulse_session')) {
    const url = req.nextUrl.clone();
    url.pathname = '/login';
    url.search = req.nextUrl.pathname === '/' ? '' : `?next=${encodeURIComponent(req.nextUrl.pathname + req.nextUrl.search)}`;
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = { matcher: ['/((?!api|login|signup|reset-password|verify-email|invite|_next|favicon.ico).*)'] };
