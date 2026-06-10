import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);

export interface JavyResult {
  ok: boolean;
  error?: string;
}

/**
 * Compile a QuickJS-safe JS file to a self-contained (statically linked) wasm
 * module via Javy. Runs the `javy-cli` entry with the current Node so it works
 * regardless of node_modules layout; the binary is downloaded and cached on
 * first use by javy-cli itself.
 */
export function javyCompile(jsPath: string, wasmPath: string): JavyResult {
  let entry: string;
  try {
    entry = require.resolve('javy-cli');
  } catch {
    return { ok: false, error: 'javy-cli is not installed' };
  }
  const result = spawnSync(process.execPath, [entry, 'compile', jsPath, '-o', wasmPath], {
    encoding: 'utf8',
    // Generous enough for javy-cli's first-use binary download on a slow
    // connection; a stall fails loudly instead of hanging compile forever.
    timeout: 120_000,
  });
  // A transport failure (ENOENT, EACCES, timeout kill) sets `error` and leaves
  // `status` null — surface the real cause, not "exited null".
  if (result.error) {
    return { ok: false, error: result.error.message };
  }
  if (result.status !== 0) {
    const detail = (result.stderr || result.stdout || `exited ${result.status}`).trim();
    return { ok: false, error: detail };
  }
  return { ok: true };
}
