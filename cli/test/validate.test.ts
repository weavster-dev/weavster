import { describe, expect, it } from 'vitest';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { checkProject } from '../src/project.js';

const here = dirname(fileURLToPath(import.meta.url));
const examples = resolve(here, '../../spec/examples/project');
const example = (name: string) => resolve(examples, name);

describe('checkProject', () => {
  it('accepts a valid config', () => {
    const result = checkProject(example('valid.weavster.yaml'));
    expect(result.ok).toBe(true);
    expect(result.errors).toEqual([]);
  });

  it('reports a missing required property', () => {
    const result = checkProject(example('invalid-missing-name.weavster.yaml'));
    expect(result.ok).toBe(false);
    expect(result.errors.join('\n')).toContain('missing required property "name"');
  });

  it('reports a wrong apiVersion', () => {
    const result = checkProject(example('invalid-bad-apiversion.weavster.yaml'));
    expect(result.ok).toBe(false);
    expect(result.errors.join('\n')).toContain('/apiVersion');
  });

  it('reports an unknown property', () => {
    const result = checkProject(example('invalid-unknown-key.weavster.yaml'));
    expect(result.ok).toBe(false);
    expect(result.errors.join('\n')).toContain('unknown property "flavor"');
  });

  it('reports a name that breaks the pattern', () => {
    const result = checkProject(example('invalid-bad-name.weavster.yaml'));
    expect(result.ok).toBe(false);
    expect(result.errors.join('\n')).toContain('/name');
  });

  it('reports a missing project file', () => {
    const result = checkProject(example('does-not-exist'));
    expect(result.ok).toBe(false);
    expect(result.file).toBeNull();
    expect(result.errors.join('\n')).toContain('no weavster.yaml');
  });
});
