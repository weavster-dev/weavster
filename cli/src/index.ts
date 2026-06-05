#!/usr/bin/env node
import { Command } from 'commander';
import { registerValidate } from './commands/validate.js';
import { registerTest } from './commands/test.js';
import { registerInit } from './commands/init.js';

const program = new Command();

program
  .name('weavster')
  .description('Config-driven integration pipelines you can validate, test, and run locally.')
  .version('0.0.0');

registerInit(program);
registerValidate(program);
registerTest(program);

program.parseAsync();
