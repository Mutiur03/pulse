import { mkdtemp, readFile, readdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { artifactName, debugIdFor, pulseSourcemaps } from '../src/index';

afterEach(() => vi.unstubAllGlobals());

async function build(plugin: ReturnType<typeof pulseSourcemaps>) {
  const dir = await mkdtemp(join(tmpdir(), 'pulse-vite-'));
  const code = 'function a(){throw new Error("x")}a();\n';
  const out = plugin.renderChunk(code, { fileName: 'assets/index-abc.js' });
  expect(out).not.toBeNull();
  await import('node:fs/promises').then((fs) => fs.mkdir(join(dir, 'assets')));
  await writeFile(join(dir, 'assets/index-abc.js'), out!.code + '//# sourceMappingURL=index-abc.js.map\n');
  await writeFile(join(dir, 'assets/index-abc.js.map'), JSON.stringify({ version: 3, sources: ['src/a.ts'], names: [], mappings: 'AAAA' }));
  const bundle = {
    'assets/index-abc.js': { type: 'chunk' as const, fileName: 'assets/index-abc.js', sourcemapFileName: 'assets/index-abc.js.map' },
    'assets/index-abc.js.map': { type: 'asset' as const, fileName: 'assets/index-abc.js.map' },
  };
  return { dir, code, out: out!, bundle };
}

describe('pulseSourcemaps', () => {
  it('injects a stable debug id after the code and into the map', async () => {
    const p = pulseSourcemaps({ release: 'web@1', silent: true });
    const { dir, code, out, bundle } = await build(p);
    const id = debugIdFor(code);
    expect(out.code.startsWith(code)).toBe(true);
    expect(out.code).toContain(`//# debugId=${id}`);
    expect(out.map).toBeNull();
    expect(p.renderChunk(out.code, { fileName: 'x.js' })).toBeNull();

    await p.writeBundle({ dir }, bundle);
    const map = JSON.parse(await readFile(join(dir, 'assets/index-abc.js.map'), 'utf8'));
    expect(map.debugId).toBe(id);
  });

  it('uploads with the token and can delete maps', async () => {
    const fetch = vi.fn(async () => new Response('[]', { status: 201 }));
    vi.stubGlobal('fetch', fetch);
    const p = pulseSourcemaps({
      release: 'web@1', url: 'http://pulse.test/', project: 3, token: 'pul_x',
      urlPrefix: '~/', deleteAfterUpload: true, silent: true,
    });
    const { dir, bundle } = await build(p);
    await p.writeBundle({ dir }, bundle);

    expect(fetch).toHaveBeenCalledOnce();
    const [endpoint, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(endpoint).toBe('http://pulse.test/v1/projects/3/releases/web%401/sourcemaps');
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer pul_x');
    expect([...(init.body as FormData).keys()]).toEqual(['~/assets/index-abc.js.map']);
    expect(await readdir(join(dir, 'assets'))).toEqual(['index-abc.js']);
  });

  it('reports upload errors', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"invalid upload token"}', { status: 401 })));
    const p = pulseSourcemaps({ release: 'web@1', project: 3, token: 'pul_bad', silent: true });
    const { dir, bundle } = await build(p);
    await expect(p.writeBundle({ dir }, bundle)).rejects.toThrow('invalid upload token');
  });

  it('names artifacts from the URL prefix', () => {
    expect(artifactName('~/', 'assets/a.js.map')).toBe('~/assets/a.js.map');
    expect(artifactName('/static/', 'a.js.map')).toBe('~/static/a.js.map');
    expect(artifactName('https://cdn.example.com/app/', 'a.js.map')).toBe('https://cdn.example.com/app/a.js.map');
  });
});
