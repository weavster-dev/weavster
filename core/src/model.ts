/**
 * Canonical document model.
 *
 * Every input format (JSON, XML, ...) is normalized into this one shape so that
 * transforms operate on a single representation instead of format-specific
 * objects. A `Node` is a tagged union of the three structural kinds; a
 * `Document` wraps a root node with metadata about where it came from.
 */

/** Leaf values the model preserves from raw input. */
export type Scalar = string | number | boolean | null;

export interface ScalarNode {
  kind: 'scalar';
  value: Scalar;
}

export interface ObjectNode {
  kind: 'object';
  /** Insertion order is preserved; it carries element order for ordered formats. */
  fields: Record<string, Node>;
}

export interface ArrayNode {
  kind: 'array';
  items: Node[];
}

export type Node = ScalarNode | ObjectNode | ArrayNode;

export type SourceFormat = 'json' | 'xml' | 'unknown';

export interface ValidationMessage {
  /** Dotted path to the offending node, or empty for document-level messages. */
  path: string;
  message: string;
}

export interface DocumentMeta {
  sourceFormat: SourceFormat;
  errors: ValidationMessage[];
}

export interface Document {
  root: Node;
  meta: DocumentMeta;
}

export function isScalar(node: Node): node is ScalarNode {
  return node.kind === 'scalar';
}

export function isObject(node: Node): node is ObjectNode {
  return node.kind === 'object';
}

export function isArray(node: Node): node is ArrayNode {
  return node.kind === 'array';
}

export function document(root: Node, meta: Partial<DocumentMeta> = {}): Document {
  return {
    root,
    meta: { sourceFormat: meta.sourceFormat ?? 'unknown', errors: meta.errors ?? [] },
  };
}

/**
 * Normalize a native JS value into a canonical node.
 *
 * This is the model's intake boundary: format packs turn text into JS values,
 * then hand the value here. `undefined` is treated as a null scalar.
 */
export function fromValue(value: unknown): Node {
  if (Array.isArray(value)) {
    return { kind: 'array', items: value.map(fromValue) };
  }
  if (value !== null && typeof value === 'object') {
    const fields: Record<string, Node> = {};
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      fields[key] = fromValue(child);
    }
    return { kind: 'object', fields };
  }
  if (value === undefined) {
    return { kind: 'scalar', value: null };
  }
  return { kind: 'scalar', value: value as Scalar };
}

/** Convert a canonical node back into a native JS value. */
export function toValue(node: Node): unknown {
  switch (node.kind) {
    case 'scalar':
      return node.value;
    case 'array':
      return node.items.map(toValue);
    case 'object': {
      const out: Record<string, unknown> = {};
      for (const [key, child] of Object.entries(node.fields)) {
        out[key] = toValue(child);
      }
      return out;
    }
  }
}
