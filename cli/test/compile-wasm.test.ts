import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { compile } from '../src/compile.js';
import { runEnvelope } from './wasmHost.js';

// End-to-end: compile the golden-path project to a real wasm artifact via Javy,
// then drive each flow module over the stdin/stdout envelope through Node's WASI
// — the same ABI the Rust engine (E3) uses. Slow (javy downloads its binary on
// first use, then compiles a ~2.5 MB module), so it gets a generous timeout.
const here = dirname(fileURLToPath(import.meta.url));
const goldenPath = resolve(here, '../../examples/golden-path');

let outDir: string;
let wasm: Buffer;

beforeAll(async () => {
  outDir = mkdtempSync(join(tmpdir(), 'wv-artifact-'));
  // Pre-seed a stale module from a "previous run" so the wipe is observable.
  mkdirSync(join(outDir, 'flows'), { recursive: true });
  writeFileSync(join(outDir, 'flows', 'stale.wasm'), 'old');
  const result = await compile(goldenPath, outDir);
  expect(result.errors).toEqual([]);
  expect(result.ok).toBe(true);
  wasm = readFileSync(join(outDir, 'flows', 'order.wasm'));
}, 120_000);

afterAll(() => rmSync(outDir, { recursive: true, force: true }));

// Drive the compiled module over the Node WASI host (test/wasmHost.ts).
const run = (input: unknown) => runEnvelope(wasm, input);

describe('compiled artifact', () => {
  it('emits a manifest and a wasm module per flow', () => {
    expect(existsSync(join(outDir, 'manifest.json'))).toBe(true);
    expect(existsSync(join(outDir, 'flows', 'order.wasm'))).toBe(true);
    // The intermediate JS bundle must not linger in the artifact.
    expect(existsSync(join(outDir, 'flows', 'order.js'))).toBe(false);
    // flows/ is wiped per run: the pre-seeded stale module is gone.
    expect(existsSync(join(outDir, 'flows', 'stale.wasm'))).toBe(false);
  });

  it('runs the flow through the wasm envelope', () => {
    const payload = JSON.stringify({ id: 'a1', first: 'Ada', last: 'Lovelace', status: 'new' });
    const result = run({ in: 'json', out: 'json', payload });
    expect(result.ok).toBe(true);
    expect(JSON.parse(result.payload as string)).toMatchObject({
      id: 'A1',
      name: 'Ada Lovelace',
      priority: 'high',
      initials: 'AL',
    });
  });

  it('converts JSON to XML through the same module', () => {
    const payload = JSON.stringify({ id: 'a1', first: 'Ada', last: 'Lovelace', status: 'new' });
    const result = run({ in: 'json', out: 'xml', payload });
    expect(result.ok).toBe(true);
    expect(result.payload).toContain('<name>Ada Lovelace</name>');
  });

  it('reports a parse failure as error{stage:"parse"} rather than crashing', () => {
    const result = run({ in: 'json', out: 'json', payload: '{not json' });
    expect(result.ok).toBe(false);
    expect(result.error?.stage).toBe('parse');
  });

  it('answers a malformed input envelope with error{stage:"envelope"}, not a trap', () => {
    const result = run('{torn envelope');
    expect(result.ok).toBe(false);
    expect(result.error?.stage).toBe('envelope');
  });
});
