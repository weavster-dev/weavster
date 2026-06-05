/**
 * Declarative transform engine.
 *
 * A flow is an ordered list of op-keyed steps. The engine runs them as a
 * mutate-in-place pipeline: the input document is cloned into a working
 * document, each step edits it via path helpers, and the working document is
 * returned. Steps operate only on the canonical model, so the same flow runs
 * regardless of whether the input arrived as JSON or XML.
 */
import { type Document, type Node, type Scalar, fromValue, isScalar } from './model.js';
import { type Segment, get, remove, set } from './path.js';

/** A single transform step. The `op` field selects the operation. */
export interface Step {
  op: string;
  [key: string]: unknown;
}

export interface Flow {
  steps: Step[];
}

/** Thrown when a step references a bad path or is otherwise malformed. */
export class TransformError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'TransformError';
  }
}

type OpFn = (working: Document, step: Step) => void;

function requirePath(step: Step, key: string): string | Segment[] {
  const value = step[key];
  if (typeof value !== 'string') throw new TransformError(`"${key}" must be a path string`);
  return value;
}

function requireNode(working: Document, path: string | Segment[]): Node {
  const node = get(working, path);
  if (node === undefined) throw new TransformError(`no value at "${String(path)}"`);
  return node;
}

/** Coerce a scalar node to a string; null becomes empty, structured nodes are an error. */
function scalarToString(node: Node, context: string): string {
  if (!isScalar(node)) throw new TransformError(`${context} must be a scalar`);
  return node.value === null ? '' : String(node.value);
}

function scalarNode(value: Scalar): Node {
  return { kind: 'scalar', value };
}

/** A concat part: a path to read, or a literal value. */
interface ConcatPart {
  path?: unknown;
  value?: unknown;
}

const OPS: Record<string, OpFn> = {
  /** Copy the value at `from` to `to`. */
  map(working, step) {
    const from = requirePath(step, 'from');
    const to = requirePath(step, 'to');
    set(working, to, structuredClone(requireNode(working, from)));
  },

  /** Move the value at `from` to `to` (copy then delete the source). */
  rename(working, step) {
    const from = requirePath(step, 'from');
    const to = requirePath(step, 'to');
    set(working, to, structuredClone(requireNode(working, from)));
    remove(working, from);
  },

  /** Set `at` to `value` only when nothing is there yet. */
  default(working, step) {
    const at = requirePath(step, 'at');
    if (!('value' in step)) throw new TransformError('"value" is required');
    if (get(working, at) === undefined) set(working, at, fromValue(step.value));
  },

  /** Join `parts` (each a `path` or literal `value`) into a string at `to`. */
  concat(working, step) {
    const to = requirePath(step, 'to');
    if (!Array.isArray(step.parts)) throw new TransformError('"parts" must be a list');
    const sep = step.sep === undefined ? '' : String(step.sep);
    const pieces = (step.parts as ConcatPart[]).map((part, i) => {
      if (typeof part?.path === 'string') {
        return scalarToString(requireNode(working, part.path), `parts[${i}] (${part.path})`);
      }
      if ('value' in part) return part.value === null ? '' : String(part.value);
      throw new TransformError(`parts[${i}] must have a "path" or "value"`);
    });
    set(working, to, scalarNode(pieces.join(sep)));
  },

  /** Apply a string function (`upper`/`lower`/`trim`) from `from` to `to` (default `from`). */
  str(working, step) {
    const from = requirePath(step, 'from');
    const to = step.to === undefined ? from : requirePath(step, 'to');
    const input = scalarToString(requireNode(working, from), `value at "${String(from)}"`);
    const fns: Record<string, (s: string) => string> = {
      upper: (s) => s.toUpperCase(),
      lower: (s) => s.toLowerCase(),
      trim: (s) => s.trim(),
    };
    const fn = fns[String(step.fn)];
    if (fn === undefined) throw new TransformError(`unknown str fn "${String(step.fn)}"`);
    set(working, to, scalarNode(fn(input)));
  },

  /** Apply a date function (`toIso`) from `from` to `to` (default `from`). */
  date(working, step) {
    const from = requirePath(step, 'from');
    const to = step.to === undefined ? from : requirePath(step, 'to');
    const input = scalarToString(requireNode(working, from), `value at "${String(from)}"`);
    if (String(step.fn) !== 'toIso')
      throw new TransformError(`unknown date fn "${String(step.fn)}"`);
    const date = new Date(input);
    if (Number.isNaN(date.getTime())) throw new TransformError(`cannot parse date "${input}"`);
    set(working, to, scalarNode(date.toISOString()));
  },
};

/** Run a flow against a document, returning a new transformed document. */
export function applyFlow(doc: Document, flow: Flow): Document {
  const working: Document = {
    root: structuredClone(doc.root),
    meta: { ...doc.meta, errors: [...doc.meta.errors] },
  };

  flow.steps.forEach((step, index) => {
    const op = OPS[step.op];
    if (op === undefined) throw new TransformError(`step ${index}: unknown op "${step.op}"`);
    try {
      op(working, step);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      throw new TransformError(`step ${index} (${step.op}): ${message}`);
    }
  });

  return working;
}
