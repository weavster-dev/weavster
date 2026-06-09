import { existsSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import type { Command } from 'commander';
import { compile } from '../compile.js';

/** Resolve a path argument (a project dir or a weavster.yaml file) to the project directory. */
function resolveProjectDir(path: string): string {
  if (existsSync(path) && statSync(path).isFile()) return dirname(path);
  return path;
}

export function registerCompile(program: Command): void {
  program
    .command('compile')
    .description('Compile enabled pipelines into a portable artifact (manifest + flow modules)')
    .argument('[path]', 'project directory or weavster.yaml (default: current directory)', '.')
    .option('-o, --out <dir>', 'artifact output directory (default: <project>/target/artifact)')
    .action(async (path: string, options: { out?: string }) => {
      const dir = resolveProjectDir(path);
      const outDir = options.out ?? join(dir, 'target', 'artifact');
      const result = await compile(dir, outDir);

      for (const error of result.errors) console.error(`✗ ${error}`);
      if (result.ok) {
        const count = result.pipelines.length;
        console.error(`✓ compiled ${count} pipeline${count === 1 ? '' : 's'} → ${result.outDir}`);
      } else {
        process.exitCode = 1;
      }
    });
}
