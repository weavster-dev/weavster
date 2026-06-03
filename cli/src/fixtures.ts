import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const FIXTURES_DIR = 'fixtures';
const INPUT_FILE = 'input.json';
const EXPECTED_FILE = 'expected.json';

export interface FixtureResult {
  name: string;
  ok: boolean;
  /** Readable expected-vs-actual diff, present when a fixture fails on output. */
  diff?: string;
  /** Load or parse failure for this fixture case. */
  error?: string;
}

export interface TestRunResult {
  ok: boolean;
  results: FixtureResult[];
  /** Project-level problems (no fixtures directory, etc.). */
  errors: string[];
}

/**
 * Run the project's transform flow over a single input document.
 *
 * M3 has no transform engine yet, so this is an identity passthrough: the
 * input is returned unchanged. The transform DSL and format packs (M4–M6)
 * replace the body here without changing the harness around it.
 */
export function runFlow(input: unknown): unknown {
  return input;
}

/** Resolve a CLI path argument to the project directory holding `fixtures/`. */
function resolveProjectDir(path: string): string {
  if (existsSync(path) && statSync(path).isFile()) {
    return join(path, '..');
  }
  return path;
}

/** Load every fixture case, run the flow, and compare output to expected. */
export function runFixtures(path: string): TestRunResult {
  const dir = resolveProjectDir(path);
  const fixturesDir = join(dir, FIXTURES_DIR);

  if (!existsSync(fixturesDir)) {
    return {
      ok: false,
      results: [],
      errors: [`no ${FIXTURES_DIR}/ directory found at ${fixturesDir}`],
    };
  }

  const cases = readdirSync(fixturesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();

  if (cases.length === 0) {
    return { ok: false, results: [], errors: [`no fixture cases found in ${fixturesDir}`] };
  }

  const results = cases.map((name) => runCase(join(fixturesDir, name), name));
  return { ok: results.every((r) => r.ok), results, errors: [] };
}

function runCase(caseDir: string, name: string): FixtureResult {
  const inputPath = join(caseDir, INPUT_FILE);
  const expectedPath = join(caseDir, EXPECTED_FILE);

  for (const [label, file] of [
    ['input', inputPath],
    ['expected', expectedPath],
  ] as const) {
    if (!existsSync(file)) return { name, ok: false, error: `missing ${label} file ${file}` };
  }

  let input: unknown;
  let expected: unknown;
  try {
    input = JSON.parse(readFileSync(inputPath, 'utf8'));
  } catch (err) {
    return { name, ok: false, error: `invalid JSON in ${INPUT_FILE}: ${String(err)}` };
  }
  try {
    expected = JSON.parse(readFileSync(expectedPath, 'utf8'));
  } catch (err) {
    return { name, ok: false, error: `invalid JSON in ${EXPECTED_FILE}: ${String(err)}` };
  }

  const actual = runFlow(input);
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
