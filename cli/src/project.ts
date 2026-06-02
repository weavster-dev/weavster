import { existsSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { parse, YAMLParseError } from 'yaml';
import { validateProject } from './schema.js';

const PROJECT_FILE = 'weavster.yaml';

export interface CheckResult {
  ok: boolean;
  /** Path to the project file that was checked, when one was found. */
  file: string | null;
  errors: string[];
}

/** Resolve a CLI path argument to a weavster.yaml file path. */
export function resolveProjectFile(path: string): string {
  if (existsSync(path) && statSync(path).isDirectory()) {
    return join(path, PROJECT_FILE);
  }
  return path;
}

/** Load, parse, and validate a project's weavster.yaml. */
export function checkProject(path: string): CheckResult {
  const file = resolveProjectFile(path);

  if (!existsSync(file)) {
    return { ok: false, file: null, errors: [`no ${PROJECT_FILE} found at ${file}`] };
  }

  let data: unknown;
  try {
    data = parse(readFileSync(file, 'utf8'));
  } catch (err) {
    const message = err instanceof YAMLParseError ? err.message : String(err);
    return { ok: false, file, errors: [`invalid YAML: ${message}`] };
  }

  const { valid, errors } = validateProject(data);
  return { ok: valid, file, errors };
}
