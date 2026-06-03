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
