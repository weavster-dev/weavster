import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { bundleFlow } from './bundle.js';
import { javyCompile } from './javy.js';
import { type Format, extFormat, loadPipeline } from './pipeline.js';
import { loadProject } from './project.js';
import { validateManifest } from './schema.js';

const MANIFEST_VERSION = '1';
const ABI_VERSION = 'javy-1';

/** A connector in the manifest. Only `file` is supported this phase. */
interface ManifestSource {
  type: 'file';
  glob: string;
  format: Format;
}
interface ManifestSink {
  type: 'file';
  path: string;
  format: Format;
}

export interface ManifestPipeline {
  name: string;
  source: ManifestSource;
  flow: string;
  sink: ManifestSink;
}

export interface Manifest {
  manifestVersion: string;
  abiVersion: string;
  pipelines: ManifestPipeline[];
}

export interface BuildResult {
  manifest: Manifest | null;
  errors: string[];
}

/** Names of the enabled pipelines in the switchboard, in declaration order. */
function enabledPipelines(path: string): { names: string[]; errors: string[] } {
  const { project, errors } = loadProject(path);
  if (project === null) return { names: [], errors };
  const switchboard = project.pipelines ?? [];
  if (switchboard.length === 0) {
    return { names: [], errors: ['no pipelines declared in weavster.yaml'] };
  }
  const names = switchboard.filter((p) => p.enabled !== false).map((p) => p.name);
  return { names, errors: [] };
}

/** Resolve one switchboard entry into a manifest pipeline (or collect its errors). */
function toManifestPipeline(
  projectDir: string,
  name: string,
): { pipeline: ManifestPipeline | null; errors: string[] } {
  const { pipeline, errors } = loadPipeline(projectDir, name);
  if (pipeline === null) return { pipeline: null, errors: errors.map((e) => `${name}: ${e}`) };

  if (pipeline.source.type !== 'file' || pipeline.sink.type !== 'file') {
    return {
      pipeline: null,
      errors: [
        `${name}: compile supports only file connectors (got source "${pipeline.source.type}", sink "${pipeline.sink.type}")`,
      ],
    };
  }
  // The pipeline schema requires `path` on file connectors, so these are present.
  const sourcePath = pipeline.source.path as string;
  const sinkPath = pipeline.sink.path as string;

  // The source format must be known to bake it into the manifest; the sink
  // falls back to the source format, mirroring `weavster run`.
  const sourceFormat = pipeline.source.format ?? extFormat(sourcePath);
  if (sourceFormat === undefined) {
    return {
      pipeline: null,
      errors: [`${name}: cannot determine source format for "${sourcePath}" — add a format`],
    };
  }
  const sinkFormat = pipeline.sink.format ?? extFormat(sinkPath) ?? sourceFormat;

  return {
    pipeline: {
      name,
      // A file path is a one-match glob; real glob fan-out is an E4 connector concern.
      source: { type: 'file', glob: sourcePath, format: sourceFormat },
      flow: pipeline.flow,
      sink: { type: 'file', path: sinkPath, format: sinkFormat },
    },
    errors: [],
  };
}

/** Build the manifest for a project's enabled pipelines, validated against the contract. */
export function buildManifest(path: string): BuildResult {
  const { names, errors } = enabledPipelines(path);
  if (errors.length > 0) return { manifest: null, errors };
  if (names.length === 0) return { manifest: null, errors: ['no enabled pipelines to compile'] };

  const pipelines: ManifestPipeline[] = [];
  const allErrors: string[] = [];
  for (const name of names) {
    const { pipeline, errors: pErrors } = toManifestPipeline(path, name);
    if (pipeline === null) allErrors.push(...pErrors);
    else pipelines.push(pipeline);
  }
  if (allErrors.length > 0) return { manifest: null, errors: allErrors };

  const manifest: Manifest = {
    manifestVersion: MANIFEST_VERSION,
    abiVersion: ABI_VERSION,
    pipelines,
  };

  const { valid, errors: schemaErrors } = validateManifest(manifest);
  if (!valid) return { manifest: null, errors: schemaErrors.map((e) => `manifest invalid: ${e}`) };
  return { manifest, errors: [] };
}

export interface CompileResult {
  ok: boolean;
  outDir: string;
  manifestPath: string | null;
  pipelines: string[];
  errors: string[];
}

/** Bundle one flow and compile it to flows/<flow>.wasm. Returns any build errors. */
async function buildFlowWasm(
  projectDir: string,
  flowsDir: string,
  flow: string,
): Promise<string[]> {
  const { code, errors } = await bundleFlow(projectDir, flow);
  if (code === null) return errors;

  // Javy needs a file input; write the bundle beside its wasm, then drop it so
  // the artifact's flows/ holds only .wasm (per docs/ARTIFACT_SPEC.md).
  const jsPath = join(flowsDir, `${flow}.js`);
  const wasmPath = join(flowsDir, `${flow}.wasm`);
  writeFileSync(jsPath, code);
  try {
    const result = javyCompile(jsPath, wasmPath);
    return result.ok ? [] : [`${flow}: javy: ${result.error}`];
  } finally {
    rmSync(jsPath, { force: true });
  }
}

/**
 * Compile a project into an artifact directory: build each flow to wasm and emit
 * manifest.json. Flows shared by multiple pipelines compile once.
 */
export async function compile(projectDir: string, outDir: string): Promise<CompileResult> {
  const { manifest, errors } = buildManifest(projectDir);
  if (manifest === null) return { ok: false, outDir, manifestPath: null, pipelines: [], errors };

  // Start from a clean flows/ so a disabled or removed pipeline's .wasm from a
  // previous run can't linger beside a manifest that no longer references it.
  const flowsDir = join(outDir, 'flows');
  rmSync(flowsDir, { recursive: true, force: true });
  mkdirSync(flowsDir, { recursive: true });

  const flows = [...new Set(manifest.pipelines.map((p) => p.flow))];
  const buildErrors: string[] = [];
  for (const flow of flows) {
    buildErrors.push(...(await buildFlowWasm(projectDir, flowsDir, flow)));
  }
  if (buildErrors.length > 0) {
    return { ok: false, outDir, manifestPath: null, pipelines: [], errors: buildErrors };
  }

  const manifestPath = join(outDir, 'manifest.json');
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);

  return {
    ok: true,
    outDir,
    manifestPath,
    pipelines: manifest.pipelines.map((p) => p.name),
    errors: [],
  };
}
