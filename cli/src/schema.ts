import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { Ajv, type ErrorObject } from 'ajv';

// The schema is the source of truth in spec/schemas/. Resolve it relative to
// this module so it loads the same whether run from src (tsx) or dist (node).
const here = dirname(fileURLToPath(import.meta.url));
const schemaPath = resolve(here, '../../spec/schemas/project.schema.json');
const projectSchema = JSON.parse(readFileSync(schemaPath, 'utf8'));

const ajv = new Ajv({ allErrors: true });
const validate = ajv.compile(projectSchema);

export interface ValidationResult {
  valid: boolean;
  errors: string[];
}

/** Validate already-parsed project data against the v0alpha1 schema. */
export function validateProject(data: unknown): ValidationResult {
  const valid = validate(data) as boolean;
  if (valid) return { valid: true, errors: [] };
  return { valid: false, errors: (validate.errors ?? []).map(formatError) };
}

/** Turn one Ajv error into a path-aware, human-readable line. */
function formatError(error: ErrorObject): string {
  const path = error.instancePath || '(root)';
  switch (error.keyword) {
    case 'required':
      return `${path}: missing required property "${error.params.missingProperty}"`;
    case 'additionalProperties':
      return `${path}: unknown property "${error.params.additionalProperty}"`;
    case 'const':
      return `${path}: must equal "${error.params.allowedValue}"`;
    default:
      return `${path}: ${error.message}`;
  }
}
