import { describe, expect, it, vi } from 'vitest';

// Mock spawnSync to exercise the transport-error and exit-status branches that
// can't be triggered with the real binary (it exists and runs).
const spawnSync = vi.hoisted(() => vi.fn());
vi.mock('node:child_process', () => ({ spawnSync }));

const { javyCompile } = await import('../src/javy.js');

describe('javyCompile (mocked spawn)', () => {
  it('surfaces a transport error instead of "exited null"', () => {
    spawnSync.mockReturnValueOnce({ error: new Error('spawn ETIMEDOUT'), status: null });
    const result = javyCompile('in.js', 'out.wasm');
    expect(result.ok).toBe(false);
    expect(result.error).toBe('spawn ETIMEDOUT');
  });

  it('surfaces stderr from a non-zero exit', () => {
    spawnSync.mockReturnValueOnce({ status: 1, stderr: 'boom\n', stdout: '' });
    const result = javyCompile('in.js', 'out.wasm');
    expect(result.ok).toBe(false);
    expect(result.error).toBe('boom');
  });

  it('reports success on a zero exit', () => {
    spawnSync.mockReturnValueOnce({ status: 0, stderr: '', stdout: '' });
    expect(javyCompile('in.js', 'out.wasm')).toEqual({ ok: true });
  });
});
