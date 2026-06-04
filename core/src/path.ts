/**
 * Path addressing for the canonical model.
 *
 * The canonical form of a path is a segment array: string segments address
 * object fields, number segments address array indices. The string form is
 * dotted with bracketed indices — `lines[0].sku` ⇄ `['lines', 0, 'sku']` —
 * and is what the transform DSL will accept from authors.
 */
import { type Document, type Node, toValue } from './model.js';

export type Segment = string | number;

const TOKEN = /\[(\d+)\]|([^.[\]]+)/g;

/** Parse a dotted/bracketed path string into segments. `''` is the root. */
export function parsePath(path: string): Segment[] {
  if (path === '') return [];
  const segments: Segment[] = [];
  for (const match of path.matchAll(TOKEN)) {
    const [, index, key] = match;
    segments.push(index !== undefined ? Number(index) : key);
  }
  return segments;
}

/** Render segments back into a dotted/bracketed path string. */
export function formatPath(segments: Segment[]): string {
  let out = '';
  for (const segment of segments) {
    if (typeof segment === 'number') {
      out += `[${segment}]`;
    } else {
      out += out === '' ? segment : `.${segment}`;
    }
  }
  return out;
}

function rootOf(target: Document | Node): Node {
  return 'root' in target ? target.root : target;
}

/** Resolve a path to its node, or `undefined` if any segment is absent. */
export function get(target: Document | Node, path: string | Segment[]): Node | undefined {
  const segments = typeof path === 'string' ? parsePath(path) : path;
  let current: Node = rootOf(target);
  for (const segment of segments) {
    if (typeof segment === 'number') {
      if (current.kind !== 'array' || segment < 0 || segment >= current.items.length) {
        return undefined;
      }
      current = current.items[segment];
    } else {
      if (current.kind !== 'object' || !(segment in current.fields)) {
        return undefined;
      }
      current = current.fields[segment];
    }
  }
  return current;
}

/** Resolve a path to a native JS value, or `undefined` if absent. */
export function getValue(target: Document | Node, path: string | Segment[]): unknown {
  const node = get(target, path);
  return node === undefined ? undefined : toValue(node);
}

/** Error thrown when a path cannot be written or removed against a node tree. */
export class PathError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'PathError';
  }
}

/**
 * Set `value` at `path`, mutating `target`. Missing object segments are created
 * along the way. Writing through a missing array index, or keying a scalar, is a
 * `PathError` — the model does not auto-grow arrays.
 */
export function set(target: Document | Node, path: string | Segment[], value: Node): void {
  const segments = typeof path === 'string' ? parsePath(path) : path;
  if (segments.length === 0) throw new PathError('cannot set the root');

  let current: Node = rootOf(target);
  for (let i = 0; i < segments.length - 1; i++) {
    const segment = segments[i];
    const here = formatPath(segments.slice(0, i + 1));
    if (typeof segment === 'number') {
      if (current.kind !== 'array' || segment < 0 || segment >= current.items.length) {
        throw new PathError(`no array index at ${here}`);
      }
      current = current.items[segment];
    } else {
      if (current.kind !== 'object') throw new PathError(`cannot descend into ${here}`);
      let next = current.fields[segment];
      if (next === undefined) {
        next = { kind: 'object', fields: {} };
        current.fields[segment] = next;
      }
      current = next;
    }
  }

  const last = segments[segments.length - 1];
  if (typeof last === 'number') {
    if (current.kind !== 'array' || last < 0 || last > current.items.length) {
      throw new PathError(`no array index at ${formatPath(segments)}`);
    }
    current.items[last] = value;
  } else {
    if (current.kind !== 'object') throw new PathError(`cannot set ${formatPath(segments)}`);
    current.fields[last] = value;
  }
}

/** Remove the node at `path`, mutating `target`. Returns whether anything was removed. */
export function remove(target: Document | Node, path: string | Segment[]): boolean {
  const segments = typeof path === 'string' ? parsePath(path) : path;
  if (segments.length === 0) throw new PathError('cannot remove the root');

  const parent = get(target, segments.slice(0, -1));
  if (parent === undefined) return false;
  const last = segments[segments.length - 1];

  if (typeof last === 'number') {
    if (parent.kind !== 'array' || last < 0 || last >= parent.items.length) return false;
    parent.items.splice(last, 1);
    return true;
  }
  if (parent.kind !== 'object' || !(last in parent.fields)) return false;
  delete parent.fields[last];
  return true;
}
