import { describe, expect, it } from 'vitest';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { runFixtures, runFlow } from '../src/fixtures.js';

const here = dirname(fileURLToPath(import.meta.url));
const harness = resolve(here, '../../tests/fixtures/harness');
const goldenPath = resolve(here, '../../examples/golden-path');

describe('runFlow', () => {
  it('passes input through unchanged (M3 identity)', () => {
    const input = { a: 1, b: [2, 3] };
    expect(runFlow(input)).toEqual(input);
  });
});

describe('runFixtures', () => {
  it('passes when output matches expected', () => {
    const run = runFixtures(resolve(harness, 'pass'));
    expect(run.ok).toBe(true);
    expect(run.errors).toEqual([]);
    expect(run.results).toEqual([{ name: 'identity', ok: true }]);
  });

  it('fails with a diff when output differs from expected', () => {
    const run = runFixtures(resolve(harness, 'fail'));
    expect(run.ok).toBe(false);
    const [result] = run.results;
    expect(result.name).toBe('changed');
    expect(result.ok).toBe(false);
    expect(result.diff).toContain('- ');
    expect(result.diff).toContain('+ ');
  });

  it('runs the golden-path example project', () => {
    const run = runFixtures(goldenPath);
    expect(run.ok).toBe(true);
    expect(run.results.map((r) => r.name)).toContain('order-passthrough');
  });

  it('reports a missing fixtures directory', () => {
    const run = runFixtures(resolve(harness, 'does-not-exist'));
    expect(run.ok).toBe(false);
    expect(run.errors.join('\n')).toContain('no fixtures/ directory');
  });
});
