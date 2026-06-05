import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { parse, YAMLParseError } from 'yaml';
import type { Flow } from '@weavster/core';
import { validateFlow } from './schema.js';

const FLOWS_DIR = 'flows';

export interface FlowLoad {
  flow: Flow | null;
  errors: string[];
}

export interface FlowCheck {
  file: string;
  ok: boolean;
  errors: string[];
}

/** Load and schema-validate a flow by name from a project's `flows/` directory. */
export function loadFlow(projectDir: string, name: string): FlowLoad {
  const file = join(projectDir, FLOWS_DIR, `${name}.yaml`);
  if (!existsSync(file)) return { flow: null, errors: [`no flow "${name}" at ${file}`] };

  let data: unknown;
  try {
    data = parse(readFileSync(file, 'utf8'));
  } catch (err) {
    const message = err instanceof YAMLParseError ? err.message : String(err);
    return { flow: null, errors: [`invalid YAML: ${message}`] };
  }

  const { valid, errors } = validateFlow(data);
  if (!valid) return { flow: null, errors };
  return { flow: data as Flow, errors: [] };
}

/** List flow names (without extension) under a project's `flows/` directory. */
export function listFlows(projectDir: string): string[] {
  const dir = join(projectDir, FLOWS_DIR);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => f.endsWith('.yaml'))
    .map((f) => f.slice(0, -'.yaml'.length))
    .sort();
}

/** Schema-validate every flow in a project. */
export function checkFlows(projectDir: string): FlowCheck[] {
  return listFlows(projectDir).map((name) => {
    const { errors } = loadFlow(projectDir, name);
    return { file: `${FLOWS_DIR}/${name}.yaml`, ok: errors.length === 0, errors };
  });
}
