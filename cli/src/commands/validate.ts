import { dirname } from 'node:path';
import type { Command } from 'commander';
import { checkProject } from '../project.js';
import { checkFlows } from '../flow.js';
import { checkPipelines } from '../pipeline.js';

export function registerValidate(program: Command): void {
  program
    .command('validate')
    .description('Validate a Weavster project config and its flows against the schema')
    .argument('[path]', 'project directory or weavster.yaml path', '.')
    .action((path: string) => {
      const result = checkProject(path);
      if (result.ok) {
        console.log(`✓ ${result.file} is valid`);
      } else {
        console.error(`✗ ${result.file ?? path}`);
        for (const error of result.errors) console.error(`  ${error}`);
        process.exitCode = 1;
      }

      const projectDir = result.file ? dirname(result.file) : path;
      for (const file of [...checkFlows(projectDir), ...checkPipelines(projectDir)]) {
        if (file.ok) {
          console.log(`✓ ${file.file} is valid`);
          continue;
        }
        console.error(`✗ ${file.file}`);
        for (const error of file.errors) console.error(`  ${error}`);
        process.exitCode = 1;
      }
    });
}
