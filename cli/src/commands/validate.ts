import type { Command } from 'commander';
import { checkProject } from '../project.js';

export function registerValidate(program: Command): void {
  program
    .command('validate')
    .description('Validate a Weavster project config against the schema')
    .argument('[path]', 'project directory or weavster.yaml path', '.')
    .action((path: string) => {
      const result = checkProject(path);
      if (result.ok) {
        console.log(`✓ ${result.file} is valid`);
        return;
      }
      console.error(`✗ ${result.file ?? path}`);
      for (const error of result.errors) {
        console.error(`  ${error}`);
      }
      process.exitCode = 1;
    });
}
