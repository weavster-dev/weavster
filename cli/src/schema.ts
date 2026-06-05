import { Ajv, type ErrorObject } from 'ajv';
// The schemas are the source of truth in spec/schemas/. Importing them inlines
// the JSON into the build, so they ship inside the published bundle.
import projectSchema from '../../spec/schemas/project.schema.json' with { type: 'json' };
import flowSchema from '../../spec/schemas/flow.schema.json' with { type: 'json' };

const ajv = new Ajv({ allErrors: true });
const validate = ajv.compile(projectSchema);
const validateFlowSchema = ajv.compile(flowSchema);

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

/** Validate already-parsed flow data against the flow schema. */
export function validateFlow(data: unknown): ValidationResult {
  const valid = validateFlowSchema(data) as boolean;
  if (valid) return { valid: true, errors: [] };
  return { valid: false, errors: (validateFlowSchema.errors ?? []).map(formatError) };
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
