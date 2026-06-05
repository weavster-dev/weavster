import { describe, expect, it } from 'vitest';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { runFixtures } from '../src/fixtures.js';

const here = dirname(fileURLToPath(import.meta.url));
const harness = resolve(here, '../../tests/fixtures/harness');
const goldenPath = resolve(here, '../../examples/golden-path');

describe('runFixtures', () => {
  it('passes when the flow output matches expected', () => {
    const run = runFixtures(resolve(harness, 'pass'));
    expect(run.ok).toBe(true);
    expect(run.errors).toEqual([]);
    expect(run.results).toEqual([{ name: 'tag/basic', ok: true }]);
  });

  it('fails with a diff when output differs from expected', () => {
    const run = runFixtures(resolve(harness, 'fail'));
    expect(run.ok).toBe(false);
    const [result] = run.results;
    expect(result.name).toBe('tag/wrong');
    expect(result.ok).toBe(false);
    expect(result.diff).toContain('- ');
    expect(result.diff).toContain('+ ');
  });

  it('errors a case whose flow cannot be loaded', () => {
    const run = runFixtures(resolve(harness, 'badflow'));
    expect(run.ok).toBe(false);
    expect(run.results[0].error).toMatch(/flow "missing"/);
  });

  it('runs the golden-path example end to end through its flow', () => {
    const run = runFixtures(goldenPath);
    expect(run.ok).toBe(true);
    expect(run.results.map((r) => r.name).sort()).toEqual([
      'order/existing-order',
      'order/new-order',
    ]);
  });

  it('reports a missing fixtures directory', () => {
    const run = runFixtures(resolve(harness, 'does-not-exist'));
    expect(run.ok).toBe(false);
    expect(run.errors.join('\n')).toContain('no fixtures/ directory');
  });
});
