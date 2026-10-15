// Reports the minified + gzipped size of a browser bundle built from the ESM
// entry, i.e. roughly what an app pays for `import { init } from '@pulse/browser'`.
import { build } from 'esbuild';
import { gzipSync } from 'node:zlib';

const result = await build({
  entryPoints: ['dist/index.js'],
  bundle: true,
  minify: true,
  format: 'esm',
  platform: 'browser',
  splitting: true,
  outdir: 'dist/.size',
  write: false,
  metafile: true,
});

let total = 0;
for (const f of result.outputFiles) {
  const gz = gzipSync(f.contents).length;
  total += gz;
  const name = f.path.split('/').pop();
  console.log(`${name.padEnd(28)} ${(f.contents.length / 1024).toFixed(1).padStart(7)} KB min  ${(gz / 1024).toFixed(1).padStart(6)} KB gzip`);
}
console.log(`total gzip: ${(total / 1024).toFixed(1)} KB`);
