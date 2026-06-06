import type { Command } from 'commander';
import { runPipelines } from '../runner.js';

export function registerRun(program: Command): void {
  program
    .command('run')
    .description('Run pipelines: read a source, transform with a flow, write a sink')
    .argument('[name]', 'pipeline name (default: all pipelines)')
    .action(async (name: string | undefined) => {
      const report = await runPipelines('.', name);

      // Status goes to stderr so a stdout sink stays pipeable.
      for (const error of report.errors) console.error(`✗ ${error}`);
      for (const result of report.results) {
        if (result.ok) {
          console.error(`✓ ${result.name}`);
          continue;
        }
        console.error(`✗ ${result.name}`);
        if (result.error) console.error(`  ${result.error}`);
      }
      if (report.results.length > 0) {
        const ran = report.results.filter((r) => r.ok).length;
        console.error(`\n${ran}/${report.results.length} pipelines ran`);
      }

      if (!report.ok) process.exitCode = 1;
    });
}
