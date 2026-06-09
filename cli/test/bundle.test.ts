import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { bundleFlow } from '../src/bundle.js';

const here = dirname(fileURLToPath(import.meta.url));
const goldenPath = resolve(here, '../../examples/golden-path');

/** Import a bundle string as a module so its `handle` can be exercised. */
async function load(code: string): Promise<{ handle: (i: unknown) => unknown }> {
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}

describe('bundleFlow', () => {
  it('bundles the golden-path flow into a sandbox-safe module', async () => {
    const { code, errors } = await bundleFlow(goldenPath, 'order');
    expect(errors).toEqual([]);
    expect(code).not.toBeNull();
    // The structuredClone polyfill rides along (Javy/QuickJS lacks it).
    expect(code).toContain('globalThis.structuredClone');
    // The S1 guard already ran inside bundleFlow; assert the obvious hazards are absent.
    expect(code).not.toMatch(/\bawait\b/);
    expect(code).not.toMatch(/from\s*['"]node:/);
  });

  it('runs the flow through the envelope: transform, convert, and parse error', async () => {
    const { code } = await bundleFlow(goldenPath, 'order');
    const { handle } = await load(code as string);
    const payload = JSON.stringify({ id: 'a1', first: 'Ada', last: 'Lovelace', status: 'new' });

    const ok = handle({ in: 'json', out: 'json', payload }) as {
      ok: boolean;
      payload: string;
    };
    expect(ok.ok).toBe(true);
    const doc = JSON.parse(ok.payload);
    expect(doc).toMatchObject({ id: 'A1', name: 'Ada Lovelace', priority: 'high', initials: 'AL' });

    const bad = handle({ in: 'json', out: 'json', payload: '{bad' }) as {
      ok: boolean;
      error: { stage: string };
    };
    expect(bad.ok).toBe(false);
    expect(bad.error.stage).toBe('parse');
  });

  it('reports a missing flow', async () => {
    const { code, errors } = await bundleFlow(goldenPath, 'nope');
    expect(code).toBeNull();
    expect(errors.join('\n')).toMatch(/no flow "nope"/);
  });
});
