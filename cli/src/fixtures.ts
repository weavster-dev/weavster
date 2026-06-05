import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { type Flow, type TransformFn, applyFlow, json, toValue } from '@weavster/core';
import { loadFlow } from './flow.js';
import { loadFunctions } from './functions.js';

const FIXTURES_DIR = 'fixtures';
const INPUT_FILE = 'input.json';
const EXPECTED_FILE = 'expected.json';

export interface FixtureResult {
  /** `<flow>/<case>` label. */
  name: string;
  ok: boolean;
  /** Readable expected-vs-actual diff, present when a fixture fails on output. */
  diff?: string;
  /** Load, parse, or transform failure for this fixture case. */
  error?: string;
}

export interface TestRunResult {
  ok: boolean;
  results: FixtureResult[];
  /** Project-level problems (no fixtures directory, etc.). */
  errors: string[];
}

/** Resolve a CLI path argument to the project directory holding `fixtures/`. */
function resolveProjectDir(path: string): string {
  if (existsSync(path) && statSync(path).isFile()) return join(path, '..');
  return path;
}

const subdirs = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();

/**
 * Run every fixture case. Fixtures are grouped by flow:
 * `fixtures/<flow>/<case>/{input,expected}.json`. Each case's input is parsed,
 * run through `flows/<flow>.yaml`, and compared to the expected output.
 */
export async function runFixtures(path: string): Promise<TestRunResult> {
  const dir = resolveProjectDir(path);
  const fixturesDir = join(dir, FIXTURES_DIR);

  if (!existsSync(fixturesDir)) {
    return {
      ok: false,
      results: [],
      errors: [`no ${FIXTURES_DIR}/ directory found at ${fixturesDir}`],
    };
  }

  const flows = subdirs(fixturesDir);
  if (flows.length === 0) {
    return { ok: false, results: [], errors: [`no fixture flows found in ${fixturesDir}`] };
  }

  const results: FixtureResult[] = [];
  for (const flowName of flows) {
    const { flow, errors } = loadFlow(dir, flowName);
    const load = flow === null ? { functions: {}, errors } : await loadFunctions(dir, flow);
    const cases = subdirs(join(fixturesDir, flowName));
    for (const caseName of cases) {
      const name = `${flowName}/${caseName}`;
      if (flow === null || load.errors.length > 0) {
        results.push({ name, ok: false, error: `flow "${flowName}": ${load.errors.join('; ')}` });
        continue;
      }
      results.push(runCase(join(fixturesDir, flowName, caseName), name, flow, load.functions));
    }
  }

  return { ok: results.every((r) => r.ok), results, errors: [] };
}

function runCase(
  caseDir: string,
  name: string,
  flow: Flow,
  functions: Record<string, TransformFn>,
): FixtureResult {
  const inputPath = join(caseDir, INPUT_FILE);
  const expectedPath = join(caseDir, EXPECTED_FILE);

  for (const [label, file] of [
    ['input', inputPath],
    ['expected', expectedPath],
  ] as const) {
    if (!existsSync(file)) return { name, ok: false, error: `missing ${label} file ${file}` };
  }

  let actual: unknown;
  let expected: unknown;
  try {
    const doc = json.parse(readFileSync(inputPath, 'utf8'));
    actual = toValue(applyFlow(doc, flow, { functions }).root);
  } catch (err) {
    return { name, ok: false, error: `${INPUT_FILE}: ${String(err)}` };
  }
  try {
    expected = JSON.parse(readFileSync(expectedPath, 'utf8'));
  } catch (err) {
    return { name, ok: false, error: `invalid JSON in ${EXPECTED_FILE}: ${String(err)}` };
  }

  if (deepEqual(actual, expected)) return { name, ok: true };
  return { name, ok: false, diff: diffJson(expected, actual) };
}

function deepEqual(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** Naive line-by-line diff of pretty-printed JSON: `-` expected, `+` actual. */
function diffJson(expected: unknown, actual: unknown): string {
  const e = JSON.stringify(expected, null, 2).split('\n');
  const a = JSON.stringify(actual, null, 2).split('\n');
  const lines: string[] = [];
  for (let i = 0; i < Math.max(e.length, a.length); i++) {
    if (e[i] === a[i]) {
      lines.push(`  ${e[i]}`);
      continue;
    }
    if (e[i] !== undefined) lines.push(`- ${e[i]}`);
    if (a[i] !== undefined) lines.push(`+ ${a[i]}`);
  }
  return lines.join('\n');
}
