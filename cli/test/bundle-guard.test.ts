import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it, vi } from 'vitest';

// Mock esbuild to exercise the empty-output guard, which a real build never
// produces (kept in its own file so bundle.test.ts uses the real esbuild).
const build = vi.hoisted(() => vi.fn());
vi.mock('esbuild', () => ({ build }));

const { bundleFlow } = await import('../src/bundle.js');

const here = dirname(fileURLToPath(import.meta.url));
const goldenPath = resolve(here, '../../examples/golden-path');

describe('bundleFlow (mocked esbuild)', () => {
  it('errors when the build yields no output files', async () => {
    build.mockResolvedValueOnce({ outputFiles: [] });
    const { code, errors } = await bundleFlow(goldenPath, 'order');
    expect(code).toBeNull();
    expect(errors.join('\n')).toMatch(/bundle produced no output/);
  });
});
