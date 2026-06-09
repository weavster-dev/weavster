import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Ajv } from 'ajv';
import { describe, expect, it } from 'vitest';

// Validate the artifact manifest contract (Engine Plan E1) directly against its
// published schema — no CLI command exists yet (`weavster compile` is E2). This
// pins the golden manifest and the schema together as the CLI↔engine contract.
const here = dirname(fileURLToPath(import.meta.url));
const spec = resolve(here, '../../spec');
const readJson = (path: string) => JSON.parse(readFileSync(resolve(spec, path), 'utf8'));

const ajv = new Ajv({ allErrors: true });
const validate = ajv.compile(readJson('schemas/manifest.schema.json'));
const example = (name: string) => readJson(`examples/manifest/${name}`);

describe('manifest schema', () => {
  it('accepts the golden manifest', () => {
    expect(validate(example('valid.manifest.json'))).toBe(true);
    expect(validate.errors).toBeNull();
  });

  it('accepts the fixture artifact manifest', () => {
    expect(validate(readJson('examples/artifact/golden-path/manifest.json'))).toBe(true);
  });

  it('rejects an unknown manifestVersion', () => {
    expect(validate(example('invalid-unknown-manifest-version.manifest.json'))).toBe(false);
    expect(validate.errors?.[0]?.instancePath).toBe('/manifestVersion');
  });

  it('rejects a pipeline missing its flow', () => {
    expect(validate(example('invalid-missing-flow.manifest.json'))).toBe(false);
    expect(validate.errors?.some((e) => e.params.missingProperty === 'flow')).toBe(true);
  });

  it('rejects an unknown connector type', () => {
    expect(validate(example('invalid-unknown-connector.manifest.json'))).toBe(false);
    expect(validate.errors?.some((e) => e.instancePath.includes('/source/type'))).toBe(true);
  });
});
