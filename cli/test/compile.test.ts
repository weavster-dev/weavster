import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { buildManifest } from '../src/compile.js';
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
