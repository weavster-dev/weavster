import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { extname, join } from 'node:path';
import { parse, YAMLParseError } from 'yaml';
import {
  type Sink,
  type Source,
  fileSink,
  fileSource,
  stdinSource,
  stdoutSink,
} from './connectors.js';
import { validatePipeline } from './schema.js';

const PIPELINES_DIR = 'pipelines';

export type Format = 'json' | 'xml';

interface ConnectorSpec {
  type: string;
  path?: string;
  format?: Format;
}

export interface Pipeline {
  source: ConnectorSpec;
  flow: string;
  sink: ConnectorSpec;
}

export interface PipelineLoad {
  pipeline: Pipeline | null;
  errors: string[];
}

export interface PipelineCheck {
  file: string;
  ok: boolean;
  errors: string[];
}

/** Load and schema-validate a pipeline by name from a project's `pipelines/` directory. */
export function loadPipeline(projectDir: string, name: string): PipelineLoad {
  const file = join(projectDir, PIPELINES_DIR, `${name}.yaml`);
  if (!existsSync(file)) return { pipeline: null, errors: [`no pipeline "${name}" at ${file}`] };

  let data: unknown;
  try {
    data = parse(readFileSync(file, 'utf8'));
  } catch (err) {
    const message = err instanceof YAMLParseError ? err.message : String(err);
    return { pipeline: null, errors: [`invalid YAML: ${message}`] };
  }

  const { valid, errors } = validatePipeline(data);
  if (!valid) return { pipeline: null, errors };
  return { pipeline: data as Pipeline, errors: [] };
}

/** List pipeline names (without extension) under a project's `pipelines/` directory. */
export function listPipelines(projectDir: string): string[] {
  const dir = join(projectDir, PIPELINES_DIR);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => f.endsWith('.yaml'))
    .map((f) => f.slice(0, -'.yaml'.length))
    .sort();
}

/** Schema-validate every pipeline in a project. */
export function checkPipelines(projectDir: string): PipelineCheck[] {
  return listPipelines(projectDir).map((name) => {
    const { errors } = loadPipeline(projectDir, name);
    return { file: `${PIPELINES_DIR}/${name}.yaml`, ok: errors.length === 0, errors };
  });
}

function extFormat(path: string): Format | undefined {
  const ext = extname(path).toLowerCase();
  if (ext === '.json') return 'json';
  if (ext === '.xml') return 'xml';
  return undefined;
}

/**
 * Resolve a source spec to a connector, the format to parse with, and whether it is bounded.
 * A bounded source (file) yields once; an unbounded source (stdin) streams until end-of-stream.
 */
export function resolveSource(
  spec: ConnectorSpec,
  projectDir: string,
): { source: Source; format: Format; bounded: boolean } {
  if (spec.type === 'stdin') {
    return { source: stdinSource(), format: spec.format as Format, bounded: false };
  }
  const path = join(projectDir, spec.path as string);
  const format = spec.format ?? extFormat(spec.path as string);
  if (format === undefined) {
    throw new Error(`cannot determine source format for "${spec.path}" — add a format`);
  }
  return { source: fileSource(path), format, bounded: true };
}

/** Resolve a sink spec to a connector and format (defaults to the source format). */
export function resolveSink(
  spec: ConnectorSpec,
  projectDir: string,
  sourceFormat: Format,
): { sink: Sink; format: Format } {
  if (spec.type === 'stdout') {
    return { sink: stdoutSink(), format: spec.format ?? sourceFormat };
  }
  const path = join(projectDir, spec.path as string);
  const format = spec.format ?? extFormat(spec.path as string) ?? sourceFormat;
  return { sink: fileSink(path), format };
}
