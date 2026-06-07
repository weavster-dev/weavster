import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { fileURLToPath } from 'node:url';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { Readable } from 'node:stream';
import { runPipelines } from '../src/runner.js';
import { checkPipelines, resolveSink, resolveSource } from '../src/pipeline.js';

const here = dirname(fileURLToPath(import.meta.url));
const goldenPath = resolve(here, '../../examples/golden-path');

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'wv-run-'));
  writeFileSync(join(dir, 'weavster.yaml'), 'apiVersion: weavster/v0alpha2\nname: t\n');
  mkdirSync(join(dir, 'flows'));
  writeFileSync(join(dir, 'flows', 'main.yaml'), 'steps:\n  - _set: { status: new }\n');
  mkdirSync(join(dir, 'pipelines'));
  mkdirSync(join(dir, 'in'));
  writeFileSync(join(dir, 'in', 'x.json'), '{ "id": 1 }');
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

const writePipeline = (name: string, body: string) =>
  writeFileSync(join(dir, 'pipelines', `${name}.yaml`), body);

describe('runPipelines', () => {
  it('reads a file source, runs the flow, and writes a file sink', async () => {
    writePipeline(
      'p',
      'source: { type: file, path: in/x.json }\nflow: main\nsink: { type: file, path: out/x.json }\n',
    );
    const report = await runPipelines(dir, 'p');
    expect(report.ok).toBe(true);
    expect(JSON.parse(readFileSync(join(dir, 'out', 'x.json'), 'utf8'))).toEqual({
      id: 1,
      status: 'new',
    });
  });

  it('runs every pipeline when no name is given', async () => {
    writePipeline(
      'p',
      'source: { type: file, path: in/x.json }\nflow: main\nsink: { type: file, path: out/x.json }\n',
    );
    const report = await runPipelines(dir);
    expect(report.ok).toBe(true);
    expect(report.results).toEqual([{ name: 'p', ok: true, documents: 1 }]);
  });

  it('fails a bounded pipeline whose only document fails to parse', async () => {
    writeFileSync(join(dir, 'in', 'bad.json'), '{ not json');
    writePipeline(
      'p',
      'source: { type: file, path: in/bad.json }\nflow: main\nsink: { type: file, path: out/x.json }\n',
    );
    const report = await runPipelines(dir, 'p');
    expect(report.ok).toBe(false);
    expect(report.results[0].error).toMatch(/document 1: invalid JSON/);
  });

  it('errors on a missing pipeline', async () => {
    const report = await runPipelines(dir, 'nope');
    expect(report.ok).toBe(false);
    expect(report.results[0].error).toMatch(/no pipeline "nope"/);
  });

  it('logs and skips a bad document on an unbounded stdin source', async () => {
    writePipeline(
      'p',
      'source: { type: stdin, format: json }\nflow: main\nsink: { type: file, path: out/x.json }\n',
    );
    const original = Object.getOwnPropertyDescriptor(process, 'stdin');
    Object.defineProperty(process, 'stdin', {
      value: Readable.from('{ "id": 1 }\n{ not json\n'),
      configurable: true,
    });
    try {
      const report = await runPipelines(dir, 'p');
      expect(report.ok).toBe(true); // a stream survives a bad document
      const [result] = report.results;
      expect(result.documents).toBe(2);
      expect(result.docErrors?.join('\n')).toMatch(/document 2: invalid JSON/);
    } finally {
      if (original) Object.defineProperty(process, 'stdin', original);
    }
  });

  it('errors when a file source is missing', async () => {
    writePipeline(
      'p',
      'source: { type: file, path: in/missing.json }\nflow: main\nsink: { type: file, path: out/x.json }\n',
    );
    const report = await runPipelines(dir, 'p');
    expect(report.ok).toBe(false);
    expect(report.results[0].error).toMatch(/no input file/);
  });
});

describe('format resolution', () => {
  it('infers source format from the file extension', () => {
    expect(resolveSource({ type: 'file', path: 'a.xml' }, dir).format).toBe('xml');
    expect(resolveSource({ type: 'file', path: 'a.json' }, dir).format).toBe('json');
  });

  it('defaults a stdout sink to the source format, and honors an override', () => {
    expect(resolveSink({ type: 'stdout' }, dir, 'xml').format).toBe('xml');
    expect(resolveSink({ type: 'stdout', format: 'json' }, dir, 'xml').format).toBe('json');
  });

  it('errors when a file source format cannot be determined', () => {
    expect(() => resolveSource({ type: 'file', path: 'a.txt' }, dir)).toThrow(
      /cannot determine source format/,
    );
  });
});

describe('checkPipelines', () => {
  it('validates the golden-path pipeline', () => {
    expect(checkPipelines(goldenPath)).toEqual([
      { file: 'pipelines/order.yaml', ok: true, errors: [] },
    ]);
  });
});
