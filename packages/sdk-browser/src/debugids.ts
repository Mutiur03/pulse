/**
 * Debug ids injected at build time (pulse-cli sourcemaps inject or
 * @pulse/vite-plugin) register themselves here, keyed by a stack trace taken
 * inside each script, so the script's URL can be read from it.
 */
type Registry = Record<string, string>;

const URL_RE = /((?:https?|file|blob|app):\/\/[^\s()]+?)(?::\d+){1,2}(?=\)|\s|$)/;

let parsed = 0;
const byUrl = new Map<string, string>();

function scan(reg: Registry): void {
  const keys = Object.keys(reg);
  if (keys.length === parsed) return;
  for (const stack of keys) {
    const m = URL_RE.exec(stack);
    if (m) byUrl.set(m[1], reg[stack]);
  }
  parsed = keys.length;
}

/** Returns {script URL: debug id} for the scripts that appear in stack. */
export function debugIdsFor(stack: string): Record<string, string> | undefined {
  const reg = (globalThis as { __pulseDebugIds?: Registry }).__pulseDebugIds;
  if (!reg || !stack) return undefined;
  scan(reg);
  let out: Record<string, string> | undefined;
  for (const [url, id] of byUrl) {
    if (stack.includes(url)) (out ??= {})[url] = id;
  }
  return out;
}

/** For tests. */
export function resetDebugIds(): void {
  parsed = 0;
  byUrl.clear();
}
