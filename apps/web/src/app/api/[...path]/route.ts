// Same-origin proxy to the Go query API. The browser only talks to this app,
// so the HTTP-only session cookie stays first-party and the API needs no CORS.
import { NextRequest } from 'next/server';

const upstream = process.env.QUERY_API_URL ?? 'http://localhost:8081';
const forwardRequest = ['cookie', 'content-type', 'user-agent', 'accept'];
const forwardResponse = ['content-type', 'set-cookie', 'retry-after'];

async function proxy(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const { path } = await ctx.params;
  const target = `${upstream}/${path.map(encodeURIComponent).join('/')}${req.nextUrl.search}`;
  const headers = new Headers();
  for (const h of forwardRequest) {
    const v = req.headers.get(h);
    if (v) headers.set(h, v);
  }
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
export const PATCH = proxy;
export const dynamic = 'force-dynamic';
