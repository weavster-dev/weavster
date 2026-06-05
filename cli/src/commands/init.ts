import type { Command } from 'commander';
import { scaffoldProject } from '../init.js';

export function registerInit(program: Command): void {
  program
    .command('init')
    .description('Scaffold a new Weavster project')
    .argument('[dir]', 'target directory', '.')
    .action((dir: string) => {
      const result = scaffoldProject(dir);
      if (!result.ok) {
        console.error(`✗ ${result.error}`);
        process.exitCode = 1;
        return;
      }
      console.log(`✓ scaffolded a Weavster project in ${dir}`);
      for (const file of result.created) console.log(`  ${file}`);
      console.log('\nnext: weavster validate && weavster test');
    });
}
