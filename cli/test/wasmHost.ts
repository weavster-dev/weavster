import { closeSync, mkdtempSync, openSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { WASI } from 'node:wasi';

/** The result envelope a flow module writes to stdout (docs/ARTIFACT_SPEC.md). */
export interface ResultEnvelope {
  ok: boolean;
  payload?: string;
  error?: { stage: string; type?: string; message?: string };
}

/**
 * The Node WASI host: drive a compiled Javy flow module once over the
 * stdin/stdout envelope ABI — the exact contract the Rust engine host uses
 * (`engine/src/host.rs`). This is the second host the E6 parity test diffs
 * against the engine; both drive the *same* wasm, so a byte difference means a
 * host disagreed, not two JS engines. A string `input` is written verbatim (to
 * exercise malformed-envelope handling); anything else is JSON-encoded.
 */
export function runEnvelope(wasm: BufferSource, input: unknown): ResultEnvelope {
  const scratch = mkdtempSync(join(tmpdir(), 'wv-wasi-'));
  const inPath = join(scratch, 'in');
  const outPath = join(scratch, 'out');
  writeFileSync(inPath, typeof input === 'string' ? input : JSON.stringify(input));
  writeFileSync(outPath, '');
  const stdin = openSync(inPath, 'r');
  const stdout = openSync(outPath, 'w');
  try {
    const wasi = new WASI({ version: 'preview1', stdin, stdout, stderr: 2, args: [], env: {} });
    const instance = new WebAssembly.Instance(new WebAssembly.Module(wasm), wasi.getImportObject());
    wasi.start(instance);
    return JSON.parse(readFileSync(outPath, 'utf8'));
  } finally {
    closeSync(stdin);
    closeSync(stdout);
    rmSync(scratch, { recursive: true, force: true });
  }
}
