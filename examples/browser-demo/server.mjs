// Static server for the demo plus two fake API routes that echo the trace
// headers they received, so you can see propagation working.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize } from 'node:path';

const root = join(import.meta.dirname, 'public');
const types = { '.html': 'text/html', '.js': 'text/javascript', '.map': 'application/json' };
const port = Number(process.env.PORT ?? 5173);

createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x');
  if (url.pathname.startsWith('/api/')) {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ path: url.pathname, traceparent: req.headers.traceparent ?? null, baggage: req.headers.baggage ?? null }));
    return;
  }
  let file = normalize(url.pathname).replace(/^(\.\.[/\\])+/, '');
  if (!extname(file)) file = '/index.html'; // SPA fallback
  try {
    const body = await readFile(join(root, file));
    res.writeHead(200, { 'Content-Type': types[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch {
    res.writeHead(404).end('not found');
  }
}).listen(port, () => console.log(`demo on http://localhost:${port} (ingest: http://localhost:8080)`));
