// Same-origin proxy to the Go query API. The browser only talks to this app,
// so the HTTP-only session cookie stays first-party and the API needs no CORS.
import { NextRequest } from 'next/server';

const upstream = process.env.QUERY_API_URL ?? 'http://localhost:8081';
// Origin and Sec-Fetch-Site let the API refuse cross-site requests (CSRF).
const forwardRequest = ['cookie', 'content-type', 'user-agent', 'accept', 'origin', 'sec-fetch-site'];
// Set this app's own proxies (a load balancer in front of it) to keep their
// X-Forwarded-For hops; otherwise only the TCP peer is forwarded.
const trustInboundForwarded = process.env.TRUST_PROXY_HEADERS === 'true';

/**
 * The X-Forwarded-For to send upstream: inbound hops when this app sits
 * behind trusted proxies, then the TCP peer recorded by peer-ip.cjs. The API
 * believes it only from addresses in its TRUSTED_PROXIES. Without the peer
 * header (e.g. next dev) nothing is forwarded and the API sees this app.
 */
function forwardedFor(req: NextRequest): string | undefined {
  const peer = req.headers.get('x-pulse-peer-ip');
  if (!peer) return undefined;
  const inbound = trustInboundForwarded ? req.headers.get('x-forwarded-for') : null;
  return inbound ? `${inbound}, ${peer}` : peer;
}
const forwardResponse = ['content-type', 'set-cookie', 'retry-after'];

async function proxy(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const { path } = await ctx.params;
  const target = `${upstream}/${path.map(encodeURIComponent).join('/')}${req.nextUrl.search}`;
  const headers = new Headers();
  for (const h of forwardRequest) {
    const v = req.headers.get(h);
    if (v) headers.set(h, v);
  }
  const xff = forwardedFor(req);
  if (xff) headers.set('x-forwarded-for', xff);
  const hasBody = req.method !== 'GET' && req.method !== 'HEAD';
  let res: Response;
  try {
    res = await fetch(target, {
      method: req.method,
      headers,
      body: hasBody ? await req.arrayBuffer() : undefined,
      redirect: 'manual',
      cache: 'no-store',
    });
  } catch {
    return Response.json({ error: 'query API unreachable' }, { status: 502 });
  }
  const out = new Headers();
  for (const h of forwardResponse) {
    if (h === 'set-cookie') res.headers.getSetCookie().forEach((c) => out.append('set-cookie', c));
    else if (res.headers.get(h)) out.set(h, res.headers.get(h)!);
  }
  return new Response(res.status === 204 ? null : await res.arrayBuffer(), { status: res.status, headers: out });
}

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
export const dynamic = 'force-dynamic';
