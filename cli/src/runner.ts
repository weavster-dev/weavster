import { existsSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { applyFlow, json, xml } from '@weavster/core';
import { loadFlow } from './flow.js';
import { loadFunctions } from './functions.js';
import {
  type Format,
  listPipelines,
  loadPipeline,
  resolveSink,
  resolveSource,
} from './pipeline.js';

const parse: Record<Format, (text: string) => ReturnType<typeof json.parse>> = {
  json: json.parse,
  xml: xml.parse,
};
const serialize: Record<Format, (doc: ReturnType<typeof json.parse>) => string> = {
  json: json.serialize,
  xml: xml.serialize,
};

export interface RunResult {
  name: string;
  ok: boolean;
  error?: string;
}

export interface RunReport {
  ok: boolean;
  results: RunResult[];
  errors: string[];
}

function resolveProjectDir(path: string): string {
  if (existsSync(path) && statSync(path).isFile()) return join(path, '..');
  return path;
}

/** Run one pipeline, or every pipeline in the project when no name is given. */
export async function runPipelines(path: string, name?: string): Promise<RunReport> {
  const dir = resolveProjectDir(path);
  const names = name ? [name] : listPipelines(dir);
  if (names.length === 0) {
    return { ok: false, results: [], errors: [`no pipelines found in ${join(dir, 'pipelines')}`] };
  }

  const results: RunResult[] = [];
  for (const pipelineName of names) results.push(await runOne(dir, pipelineName));
  return { ok: results.every((r) => r.ok), results, errors: [] };
}

async function runOne(dir: string, name: string): Promise<RunResult> {
  try {
    const { pipeline, errors } = loadPipeline(dir, name);
    if (pipeline === null) return { name, ok: false, error: errors.join('; ') };

    const { flow, errors: flowErrors } = loadFlow(dir, pipeline.flow);
    if (flow === null)
      return { name, ok: false, error: `flow "${pipeline.flow}": ${flowErrors.join('; ')}` };

    const { functions, errors: fnErrors } = await loadFunctions(dir, flow);
    if (fnErrors.length > 0) return { name, ok: false, error: fnErrors.join('; ') };

    const { source, format: inFormat } = resolveSource(pipeline.source, dir);
    const { sink, format: outFormat } = resolveSink(pipeline.sink, dir, inFormat);

    const doc = parse[inFormat](await source.read());
    await sink.write(serialize[outFormat](applyFlow(doc, flow, { functions })));
    return { name, ok: true };
  } catch (err) {
    return { name, ok: false, error: err instanceof Error ? err.message : String(err) };
  }
}
