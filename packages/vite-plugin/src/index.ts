import { createHash } from 'node:crypto';
import { readFile, rm, writeFile } from 'node:fs/promises';
import { join, posix } from 'node:path';

export interface PulseSourcemapsOptions {
  /** Release the maps belong to; must match the SDK's `release`. */
  release: string;
  /** Pulse API base URL. Default PULSE_URL or http://localhost:8081. */
  url?: string;
  /** Project id. Default PULSE_PROJECT. */
  project?: string | number;
  /** Upload token from project settings. Default PULSE_UPLOAD_TOKEN. */
  token?: string;
  /**
   * URL prefix the output directory is served from: "~/assets" matches any
   * host, or a full URL. Default "~/".
   */
  urlPrefix?: string;
  /** Add debug ids to scripts and maps. Default true. */
  inject?: boolean;
  /** Upload after the build. Default true when a token is set. */
  upload?: boolean;
  /** Remove .map files from the output after uploading. Default false. */
  deleteAfterUpload?: boolean;
  /** Silence the build log. */
  silent?: boolean;
}

/** Minimal shapes of the Rollup/Vite hooks this plugin uses. */
interface OutputChunk {
  type: 'chunk';
  fileName: string;
  sourcemapFileName?: string | null;
}
interface OutputAsset {
  type: 'asset';
  fileName: string;
}
type Bundle = Record<string, OutputChunk | OutputAsset>;

export interface Plugin {
  name: string;
  apply: 'build';
  enforce: 'post';
  renderChunk(code: string, chunk: { fileName: string }): { code: string; map: null } | null;
  writeBundle(options: { dir?: string; file?: string }, bundle: Bundle): Promise<void>;
}

/** Same id derivation as pulse-cli: stable for the same output. */
export function debugIdFor(code: string): string {
  const h = createHash('sha256').update(code).digest();
  h[6] = (h[6] & 0x0f) | 0x40;
  h[8] = (h[8] & 0x3f) | 0x80;
  const x = h.subarray(0, 16).toString('hex');
  return `${x.slice(0, 8)}-${x.slice(8, 12)}-${x.slice(12, 16)}-${x.slice(16, 20)}-${x.slice(20, 32)}`;
}

/**
 * Registers the script's debug id under a stack trace taken inside it; the
 * browser SDK reads the script URL from that stack. Appended after all code
 * so no mapping moves.
 */
export function snippet(id: string): string {
  return (
    ';!function(){try{var g=typeof globalThis!="undefined"?globalThis:self,s=(new Error).stack;' +
    `s&&((g.__pulseDebugIds=g.__pulseDebugIds||{})[s]="${id}")}catch(e){}}();` +
    `\n//# debugId=${id}\n`
  );
}

/** The artifact name of a map: the URL prefix plus its path in the output. */
export function artifactName(prefix: string, fileName: string): string {
  const rel = fileName.split('\\').join('/');
  if (prefix.includes('://')) return prefix.replace(/\/+$/, '') + '/' + rel;
  const p = prefix.replace(/^~/, '').replace(/^\/+|\/+$/g, '');
  return p ? `~/${p}/${rel}` : `~/${rel}`;
}

const DEBUG_ID_RE = /\/\/# debugId=([0-9a-fA-F-]{32,36})/;

export function pulseSourcemaps(opts: PulseSourcemapsOptions): Plugin {
  const env = (typeof process !== 'undefined' ? process.env : {}) as Record<string, string | undefined>;
  const url = (opts.url ?? env.PULSE_URL ?? 'http://localhost:8081').replace(/\/+$/, '');
  const project = String(opts.project ?? env.PULSE_PROJECT ?? '');
  const token = opts.token ?? env.PULSE_UPLOAD_TOKEN ?? '';
  const inject = opts.inject !== false;
  const upload = opts.upload ?? token !== '';
  const log = (msg: string) => {
    if (!opts.silent) console.log(`[pulse] ${msg}`);
  };
  const ids = new Map<string, string>();

  return {
    name: 'pulse-sourcemaps',
    apply: 'build',
    enforce: 'post',

    renderChunk(code, chunk) {
      if (!inject || DEBUG_ID_RE.test(code)) return null;
      const id = debugIdFor(code);
      ids.set(chunk.fileName, id);
      // map: null tells the bundler the existing map still holds.
      return { code: code + (code.endsWith('\n') ? '' : '\n') + snippet(id), map: null };
    },

    async writeBundle(options, bundle) {
      const dir = options.dir ?? (options.file ? posix.dirname(options.file) : 'dist');
      const maps: { name: string; path: string; data: string }[] = [];
      for (const out of Object.values(bundle)) {
        if (out.type !== 'chunk') continue;
        const mapFile = out.sourcemapFileName ?? `${out.fileName}.map`;
        if (!(mapFile in bundle)) continue;
        const path = join(dir, mapFile);
        let data = await readFile(path, 'utf8');
        const id = ids.get(out.fileName);
        if (id) {
          const obj = JSON.parse(data) as Record<string, unknown>;
          obj.debugId = id;
          data = JSON.stringify(obj);
          await writeFile(path, data);
        }
        maps.push({ name: artifactName(opts.urlPrefix ?? '~/', mapFile), path, data });
      }
      if (inject) log(`injected debug ids into ${ids.size} chunks`);
      if (!upload || maps.length === 0) return;
      if (!project || !token || !opts.release) {
        throw new Error('[pulse] release, project and token are required to upload source maps');
      }
      const form = new FormData();
      for (const m of maps) form.append(m.name, new Blob([m.data], { type: 'application/json' }), posix.basename(m.path));
      const endpoint = `${url}/v1/projects/${encodeURIComponent(project)}/releases/${encodeURIComponent(opts.release)}/sourcemaps`;
      const res = await fetch(endpoint, { method: 'POST', body: form, headers: { Authorization: `Bearer ${token}` } });
      if (res.status !== 201) {
        const body = (await res.json().catch(() => ({}))) as { error?: string };
        throw new Error(`[pulse] source map upload failed (${res.status})${body.error ? `: ${body.error}` : ''}`);
      }
      log(`uploaded ${maps.length} source maps to release ${opts.release}`);
      if (opts.deleteAfterUpload) {
        await Promise.all(maps.map((m) => rm(m.path, { force: true })));
        log('deleted source maps from the output');
      }
    },
  };
}

export default pulseSourcemaps;
