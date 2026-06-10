import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { buildManifest, compile } from '../src/compile.js';
import { javyCompile } from '../src/javy.js';
import { validateManifest } from '../src/schema.js';

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'wv-compile-'));
  mkdirSync(join(dir, 'pipelines'));
  const pipeline = (name: string, body: string) =>
    writeFileSync(join(dir, 'pipelines', `${name}.yaml`), body);
  pipeline(
    'order',
    'source: { type: file, path: in/order.json }\nflow: order\nsink: { type: file, path: out/order.json }\n',
  );
  pipeline(
    'legacy',
    'source: { type: file, path: in/legacy.json }\nflow: legacy\nsink: { type: file, path: out/legacy.json }\n',
  );
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

const writeProject = (switchboard: string) =>
  writeFileSync(
    join(dir, 'weavster.yaml'),
    `apiVersion: weavster/v0alpha2\nname: t\npipelines:\n${switchboard}`,
  );

describe('buildManifest', () => {
  it('excludes disabled pipelines from the manifest', () => {
    writeProject('  - name: order\n    enabled: true\n  - name: legacy\n    enabled: false\n');
    const { manifest, errors } = buildManifest(dir);
    expect(errors).toEqual([]);
    expect(manifest?.pipelines.map((p) => p.name)).toEqual(['order']);
  });

  it('treats a missing enabled flag as enabled', () => {
    writeProject('  - name: order\n');
    const { manifest } = buildManifest(dir);
    expect(manifest?.pipelines.map((p) => p.name)).toEqual(['order']);
  });

  it('maps the source path to a glob and resolves formats', () => {
    writeProject('  - name: order\n');
    const { manifest } = buildManifest(dir);
    expect(manifest?.pipelines[0]).toEqual({
      name: 'order',
      source: { type: 'file', glob: 'in/order.json', format: 'json' },
      flow: 'order',
      sink: { type: 'file', path: 'out/order.json', format: 'json' },
    });
  });

  it('produces a manifest that validates against the contract schema', () => {
    writeProject('  - name: order\n');
    const { manifest } = buildManifest(dir);
    expect(validateManifest(manifest).valid).toBe(true);
  });

  it('errors when no pipelines are declared', () => {
    writeFileSync(join(dir, 'weavster.yaml'), 'apiVersion: weavster/v0alpha2\nname: t\n');
    const { manifest, errors } = buildManifest(dir);
    expect(manifest).toBeNull();
    expect(errors.join('\n')).toMatch(/no pipelines declared/);
  });

  it('errors when the source format cannot be determined', () => {
    writeFileSync(
      join(dir, 'pipelines', 'order.yaml'),
      'source: { type: file, path: in/order.txt }\nflow: order\nsink: { type: file, path: out/order.json }\n',
    );
    writeProject('  - name: order\n');
    const { manifest, errors } = buildManifest(dir);
    expect(manifest).toBeNull();
    expect(errors.join('\n')).toMatch(/cannot determine source format/);
  });

  it('errors when weavster.yaml is invalid YAML', () => {
    writeFileSync(join(dir, 'weavster.yaml'), 'pipelines: [unclosed');
    const { manifest, errors } = buildManifest(dir);
    expect(manifest).toBeNull();
    expect(errors.join('\n')).toMatch(/invalid YAML/);
  });

  it('rejects a non-file connector', () => {
    writeFileSync(
      join(dir, 'pipelines', 'order.yaml'),
      'source: { type: stdin, format: json }\nflow: order\nsink: { type: file, path: out/order.json }\n',
    );
    writeProject('  - name: order\n');
    const { manifest, errors } = buildManifest(dir);
    expect(manifest).toBeNull();
    expect(errors.join('\n')).toMatch(/only file connectors/);
  });
});

describe('validateManifest', () => {
  it('rejects data that does not match the contract schema', () => {
    const { valid, errors } = validateManifest({});
    expect(valid).toBe(false);
    expect(errors.length).toBeGreaterThan(0);
  });
});

describe('compile error paths', () => {
  it('fails compile (without invoking Javy) when a flow bundle is rejected', async () => {
    // An async `_ts` function trips the sandbox guard during bundling, so
    // compile must fail before any wasm is written.
    mkdirSync(join(dir, 'flows'));
    mkdirSync(join(dir, 'functions'));
    writeFileSync(join(dir, 'flows', 'order.yaml'), 'steps:\n  - _ts: { module: fn }\n');
    writeFileSync(join(dir, 'functions', 'fn.ts'), 'export default async (v: unknown) => v;\n');
    writeProject('  - name: order\n');

    const out = join(dir, 'target', 'artifact');
    const result = await compile(dir, out);
    expect(result.ok).toBe(false);
    expect(result.errors.join('\n')).toMatch(/not sandbox-safe/);
    expect(existsSync(join(out, 'manifest.json'))).toBe(false);
  });
});

describe('javyCompile', () => {
  it('reports a failed compile with javy stderr detail', () => {
    const result = javyCompile(join(dir, 'missing.js'), join(dir, 'out.wasm'));
    expect(result.ok).toBe(false);
    expect(result.error).toBeTruthy();
  });
});
