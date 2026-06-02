#!/usr/bin/env node
import { Command } from 'commander';
import { registerValidate } from './commands/validate.js';

const program = new Command();

program
  .name('weavster')
  .description('Config-driven integration pipelines you can validate, test, and run locally.')
  .version('0.0.0');

registerValidate(program);

program.parseAsync();
