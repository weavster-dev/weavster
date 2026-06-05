/**
 * v0alpha2 transform engine.
 *
 * A flow is a pipeline of single-key `_op` steps run against a working document
 * (a clone of the input). Steps are **patch by default**: `_set` and `_unset`
 * change only the paths they name and leave the rest of the document intact.
 * Values are expressions (see `expr.ts`).
 */
import { type Document, type Node, fromValue } from '../model.js';
import { get, remove, set } from '../path.js';
import { type Ctx, evalExpr } from './expr.js';
import { TransformError } from './errors.js';

export { TransformError } from './errors.js';

/** A single transform step: one `_`-prefixed operator key mapped to its argument. */
export type Step = Record<string, unknown>;

export interface Flow {
  steps: Step[];
}

export interface RunOptions {
  functions?: Record<string, (value: unknown) => unknown>;
}

type StructuralOp = (working: Document, arg: unknown, ctx: Ctx) => void;

function asRecord(arg: unknown, op: string): Record<string, unknown> {
  if (arg === null || typeof arg !== 'object' || Array.isArray(arg)) {
    throw new TransformError(`"${op}" expects a map of paths to values`);
  }
  return arg as Record<string, unknown>;
}

const STRUCTURAL: Record<string, StructuralOp> = {
  /** Patch: set each path to its evaluated expression. Missing (undefined) values are skipped. */
  _set(working, arg, ctx) {
    const entries = Object.entries(asRecord(arg, '_set')).map(
      ([path, expr]) => [path, evalExpr(expr, ctx)] as const,
    );
    for (const [path, value] of entries) {
      if (value !== undefined) set(working, path, fromValue(value) as Node);
    }
  },

  /** Remove each listed path. */
  _unset(working, arg) {
    if (!Array.isArray(arg)) throw new TransformError('"_unset" expects a list of paths');
    for (const path of arg) {
      if (typeof path !== 'string') throw new TransformError('"_unset" paths must be strings');
      remove(working, path);
    }
  },

  /** Move each `from` path to its `to` path. Missing sources are skipped. */
  _rename(working, arg) {
    for (const [from, to] of Object.entries(asRecord(arg, '_rename'))) {
      if (typeof to !== 'string')
        throw new TransformError('"_rename" targets must be path strings');
      const node = get(working, from);
      if (node === undefined) continue;
      set(working, to, structuredClone(node));
      remove(working, from);
    }
  },

  /** Append an evaluated value to the array at `to` (creating it if absent). */
  _append(working, arg, ctx) {
    const spec = asRecord(arg, '_append');
    if (typeof spec.to !== 'string') throw new TransformError('"_append" needs a "to" path string');
    const value = fromValue(evalExpr(spec.value, ctx)) as Node;
    const existing = get(working, spec.to);
    if (existing === undefined) {
      set(working, spec.to, { kind: 'array', items: [value] });
    } else if (existing.kind === 'array') {
      existing.items.push(value);
    } else {
      throw new TransformError(`"_append" target "${spec.to}" is not an array`);
    }
  },

  /** Reshape: build a fresh document from only the named paths (strict projection). */
  _select(working, arg, ctx) {
    const entries = Object.entries(asRecord(arg, '_select')).map(
      ([path, expr]) => [path, evalExpr(expr, ctx)] as const,
    );
    const fresh: Document = { root: { kind: 'object', fields: {} }, meta: working.meta };
    for (const [path, value] of entries) {
      if (value !== undefined) set(fresh, path, fromValue(value) as Node);
    }
    working.root = fresh.root;
  },

  /** Run `then` when `cond` is truthy, otherwise `else`. */
  _when(working, arg, ctx) {
    const spec = asRecord(arg, '_when');
    if (!Array.isArray(spec.then)) throw new TransformError('"_when" needs a "then" step list');
    if (spec.else !== undefined && !Array.isArray(spec.else)) {
      throw new TransformError('"_when" "else" must be a step list');
    }
    const branch = Boolean(evalExpr(spec.cond, ctx))
      ? (spec.then as Step[])
      : ((spec.else as Step[] | undefined) ?? []);
    runSteps(working, branch, ctx);
  },
};

function runSteps(working: Document, steps: Step[], ctx: Ctx): void {
  steps.forEach((step, index) => {
    const keys = Object.keys(step);
    if (keys.length !== 1) {
      throw new TransformError(`step ${index}: a step must have exactly one operator key`);
    }
    const op = keys[0];
    const impl = STRUCTURAL[op];
    if (impl === undefined) throw new TransformError(`step ${index}: unknown operator "${op}"`);
    try {
      impl(working, step[op], ctx);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      throw new TransformError(`step ${index} (${op}): ${message}`);
    }
  });
}

/** Run a flow against a document, returning a new transformed document. */
export function applyFlow(doc: Document, flow: Flow, _options: RunOptions = {}): Document {
  const working: Document = {
    root: structuredClone(doc.root),
    meta: { ...doc.meta, errors: [...doc.meta.errors] },
  };
  runSteps(working, flow.steps, { working });
  return working;
}
