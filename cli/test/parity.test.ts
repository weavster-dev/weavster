import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { compile } from '../src/compile.js';
import { runEnvelope } from './wasmHost.js';

// E6 parity guardrail (RFC 0003 slice 6). Drive the SAME compiled order.wasm
// through both harnesses — the Node WASI host (test/wasmHost.ts) and the Rust
// engine binary — and assert byte-equal output. Because both drive one wasm,
// a difference means the two *hosts* disagree on I/O, envelope encoding, or
// serialization; it is not a comparison of two JS engines.
//
// Gated on WEAVSTER_ENGINE_BIN (the built engine binary), so the suite — and
// its slow Javy compile — runs only in the dedicated CI parity job, not on
// every local `pnpm test`. Build the binary with `cargo build -p
// weavster-engine` and point the env var at target/{debug,release}/weavster-engine.
const here = dirname(fileURLToPath(import.meta.url));
const goldenPath = resolve(here, '../../examples/golden-path');
const engineBin = process.env.WEAVSTER_ENGINE_BIN;

describe.skipIf(!engineBin)('host parity (E6)', () => {
  let outDir: string;
  let wasm: Buffer;
  let inputDoc: string;

  beforeAll(async () => {
    outDir = mkdtempSync(join(tmpdir(), 'wv-parity-'));
    const result = await compile(goldenPath, outDir);
    expect(result.errors).toEqual([]);
    expect(result.ok).toBe(true);
    wasm = readFileSync(join(outDir, 'flows', 'order.wasm'));
    inputDoc = readFileSync(join(goldenPath, 'in', 'order.json'), 'utf8');
  }, 120_000);

  afterAll(() => rmSync(outDir, { recursive: true, force: true }));

  it('the TS WASI host and the Rust engine produce byte-equal output', () => {
    // Host A — Node WASI: golden input → result envelope payload.
    const ts = runEnvelope(wasm, { in: 'json', out: 'json', payload: inputDoc });
    expect(ts.ok, JSON.stringify(ts.error)).toBe(true);

    // Host B — Rust engine: stage the input under the artifact (the connector
    // root), boot from a mounted weavster.yaml, then read the sink file.
    mkdirSync(join(outDir, 'in'), { recursive: true });
    writeFileSync(join(outDir, 'in', 'order.json'), inputDoc);
    const config = join(outDir, 'weavster.yaml');
    writeFileSync(config, 'apiVersion: weavster/v0alpha2\nname: golden-path\n');
    const run = spawnSync(engineBin as string, ['-c', config, '--artifact', outDir], {
      encoding: 'utf8',
    });
    expect(run.status, run.stderr).toBe(0);
    const rust = readFileSync(join(outDir, 'out', 'order.json'), 'utf8');

    // The guardrail: same wasm, two hosts, identical bytes.
    expect(rust).toBe(ts.payload);
  });
});
