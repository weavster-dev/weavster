import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { projectName, scaffoldProject } from '../src/init.js';
import { checkProject } from '../src/project.js';
import { checkFlows } from '../src/flow.js';
import { runFixtures } from '../src/fixtures.js';

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'weavster-init-'));
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe('projectName', () => {
  it('derives a kebab name from the directory, with a fallback', () => {
    expect(projectName('/tmp/My Orders')).toBe('my-orders');
    expect(projectName('/tmp/123')).toBe('123');
  });
});

describe('scaffoldProject', () => {
  it('writes the starter files', () => {
    const result = scaffoldProject(dir);
    expect(result.ok).toBe(true);
    expect(result.created).toContain('weavster.yaml');
    expect(result.created).toContain('flows/main.yaml');
  });

  it('produces a project that validates', () => {
    scaffoldProject(dir);
    expect(checkProject(dir).ok).toBe(true);
    expect(checkFlows(dir).every((f) => f.ok)).toBe(true);
  });

  it('produces fixtures that pass weavster test', async () => {
    scaffoldProject(dir);
    const run = await runFixtures(dir);
    expect(run.ok).toBe(true);
    expect(run.results).toEqual([{ name: 'main/basic', ok: true }]);
  });

  it('refuses to overwrite an existing project', () => {
    scaffoldProject(dir);
    const second = scaffoldProject(dir);
    expect(second.ok).toBe(false);
    expect(second.error).toMatch(/already contains/);
  });
});
