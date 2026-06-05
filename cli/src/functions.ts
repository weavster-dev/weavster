import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { createJiti } from 'jiti';
import type { Flow, Step, TransformFn } from '@weavster/core';

const FUNCTIONS_DIR = 'functions';

/** Collect the module names referenced by `_ts` steps, recursing into `_when` branches. */
function collectModules(steps: Step[]): string[] {
  const names = new Set<string>();
  const walk = (list: Step[]) => {
    for (const step of list) {
      const ts = step._ts as { module?: unknown } | undefined;
      if (ts && typeof ts.module === 'string') names.add(ts.module);
      const when = step._when as { then?: unknown; else?: unknown } | undefined;
      if (when) {
        if (Array.isArray(when.then)) walk(when.then as Step[]);
        if (Array.isArray(when.else)) walk(when.else as Step[]);
      }
    }
  };
  walk(steps);
  return [...names];
}

export interface FunctionsLoad {
  functions: Record<string, TransformFn>;
  errors: string[];
}

/** Load the custom TypeScript functions a flow references from `functions/<name>.ts`. */
export async function loadFunctions(projectDir: string, flow: Flow): Promise<FunctionsLoad> {
  const modules = collectModules(flow.steps);
  if (modules.length === 0) return { functions: {}, errors: [] };

  const jiti = createJiti(import.meta.url);
  const functions: Record<string, TransformFn> = {};
  const errors: string[] = [];

  for (const name of modules) {
    const file = resolve(projectDir, FUNCTIONS_DIR, `${name}.ts`);
    if (!existsSync(file)) {
      errors.push(`no function "${name}" at ${file}`);
      continue;
    }
    try {
      const fn = await jiti.import(file, { default: true });
      if (typeof fn !== 'function') {
        errors.push(`function "${name}" has no default export function`);
        continue;
      }
      functions[name] = fn as TransformFn;
    } catch (err) {
      errors.push(`function "${name}": ${String(err)}`);
    }
  }

  return { functions, errors };
}
