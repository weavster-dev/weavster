import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';

const PROJECT_FILE = 'weavster.yaml';

/** Derive a schema-valid project name (kebab-case) from the target directory. */
export function projectName(dir: string): string {
  const slug = basename(resolve(dir))
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return /^[a-z0-9]/.test(slug) ? slug : 'my-project';
}

function templates(name: string): Record<string, string> {
  return {
    [PROJECT_FILE]: `apiVersion: weavster/v0alpha2\nname: ${name}\ndescription: A new Weavster project.\n`,
    'flows/main.yaml':
      '# Your first flow. Steps run top to bottom; this one adds a field.\nsteps:\n  - _set:\n      status: new\n',
    'fixtures/main/basic/input.json': '{\n  "id": "demo-1"\n}\n',
    'fixtures/main/basic/expected.json': '{\n  "id": "demo-1",\n  "status": "new"\n}\n',
    'README.md': `# ${name}\n\nA Weavster project.\n\n- \`weavster validate\` — check the config and flows\n- \`weavster test\` — run fixtures through flows\n`,
  };
}

export interface ScaffoldResult {
  ok: boolean;
  /** Paths written, relative to the project directory. */
  created: string[];
  error?: string;
}

/** Write a minimal starter project into `dir`. Refuses to overwrite an existing project. */
export function scaffoldProject(dir: string): ScaffoldResult {
  if (existsSync(join(dir, PROJECT_FILE))) {
    return { ok: false, created: [], error: `${dir} already contains a ${PROJECT_FILE}` };
  }
  const created: string[] = [];
  for (const [rel, content] of Object.entries(templates(projectName(dir)))) {
    const path = join(dir, rel);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, content);
    created.push(rel);
  }
  return { ok: true, created };
}
