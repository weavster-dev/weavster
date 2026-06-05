import type { Command } from 'commander';
import { runFixtures } from '../fixtures.js';

export function registerTest(program: Command): void {
  program
    .command('test')
    .description("Run a project's fixtures and compare output against expected")
    .argument('[path]', 'project directory', '.')
    .action(async (path: string) => {
      const run = await runFixtures(path);

      for (const error of run.errors) {
        console.error(`✗ ${error}`);
      }

      for (const result of run.results) {
        if (result.ok) {
          console.log(`✓ ${result.name}`);
          continue;
        }
        console.error(`✗ ${result.name}`);
        if (result.error) console.error(`  ${result.error}`);
        if (result.diff) console.error(indent(result.diff));
      }

      if (run.results.length > 0) {
        const passed = run.results.filter((r) => r.ok).length;
        console.log(`\n${passed}/${run.results.length} fixtures passed`);
      }

      if (!run.ok) process.exitCode = 1;
    });
}

function indent(block: string): string {
  return block
    .split('\n')
    .map((line) => `  ${line}`)
    .join('\n');
}
