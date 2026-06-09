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

/** A pipeline entry in the weavster.yaml switchboard. */
export interface SwitchboardEntry {
  name: string;
  enabled?: boolean;
}

export interface Project {
  apiVersion: string;
  name: string;
  description?: string;
  pipelines?: SwitchboardEntry[];
}

export interface ProjectLoad {
  project: Project | null;
  file: string | null;
  errors: string[];
}

/** Load, parse, and validate a project's weavster.yaml. */
export function checkProject(path: string): CheckResult {
  const { project, file, errors } = loadProject(path);
  return { ok: project !== null, file, errors };
}

/** Load, parse, and schema-validate a project, returning the parsed data. */
export function loadProject(path: string): ProjectLoad {
  const file = resolveProjectFile(path);

  if (!existsSync(file)) {
    return { project: null, file: null, errors: [`no ${PROJECT_FILE} found at ${file}`] };
  }

  let data: unknown;
  try {
    data = parse(readFileSync(file, 'utf8'));
  } catch (err) {
    const message = err instanceof YAMLParseError ? err.message : String(err);
    return { project: null, file, errors: [`invalid YAML: ${message}`] };
  }

  const { valid, errors } = validateProject(data);
  if (!valid) return { project: null, file, errors };
  return { project: data as Project, file, errors: [] };
}
