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
  /** Number of documents the source yielded. */
  documents: number;
  /** A startup or bounded-source failure that ends the pipeline. */
  error?: string;
  /** Per-document failures on an unbounded source (logged, did not end the pipeline). */
  docErrors?: string[];
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

const message = (err: unknown): string => (err instanceof Error ? err.message : String(err));

async function runOne(dir: string, name: string): Promise<RunResult> {
  // Startup: load and resolve everything before the loop; any failure ends the pipeline.
  const { pipeline, errors } = loadPipeline(dir, name);
  if (pipeline === null) return { name, ok: false, documents: 0, error: errors.join('; ') };

  const { flow, errors: flowErrors } = loadFlow(dir, pipeline.flow);
  if (flow === null) {
    return {
      name,
      ok: false,
      documents: 0,
      error: `flow "${pipeline.flow}": ${flowErrors.join('; ')}`,
    };
  }

  const { functions, errors: fnErrors } = await loadFunctions(dir, flow);
  if (fnErrors.length > 0) return { name, ok: false, documents: 0, error: fnErrors.join('; ') };

  let source: { documents(): AsyncIterable<string> };
  let inFormat: Format;
  let bounded: boolean;
  let sink: { write(text: string): Promise<void> };
  let outFormat: Format;
  try {
    ({ source, format: inFormat, bounded } = resolveSource(pipeline.source, dir));
    ({ sink, format: outFormat } = resolveSink(pipeline.sink, dir, inFormat));
  } catch (err) {
    return { name, ok: false, documents: 0, error: message(err) };
  }

  // The run loop: one iteration per document the source yields.
  let documents = 0;
  const docErrors: string[] = [];
  try {
    for await (const text of source.documents()) {
      documents += 1;
      try {
        const out = applyFlow(parse[inFormat](text), flow, { functions });
        await sink.write(serialize[outFormat](out));
      } catch (err) {
        const scoped = `document ${documents}: ${message(err)}`;
        // Bounded source: the only document failed, so the pipeline fails. Unbounded:
        // log it and keep the stream alive.
        if (bounded) return { name, ok: false, documents, error: scoped };
        docErrors.push(scoped);
      }
    }
  } catch (err) {
    // The source failed to open or stream — a startup-class failure.
    return { name, ok: false, documents, error: message(err) };
  }

  return { name, ok: true, documents, docErrors: docErrors.length > 0 ? docErrors : undefined };
}
