/**
 * Declarative transform engine.
 *
 * A flow is an ordered list of op-keyed steps. The engine runs them as a
 * mutate-in-place pipeline: the input document is cloned into a working
 * document, each step edits it via path helpers, and the working document is
 * returned. Steps operate only on the canonical model, so the same flow runs
 * regardless of whether the input arrived as JSON or XML.
 */
import { type Document, type Node, fromValue } from './model.js';
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
