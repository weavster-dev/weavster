/**
 * JSON format pack.
 *
 * Format packs own the text⇄value boundary; the canonical model owns
 * value⇄node. So `parse` is `JSON.parse` followed by `fromValue`, and
 * `serialize` is `toValue` followed by `JSON.stringify`.
 */
import { type Document, type Node, document, fromValue, toValue } from '../model.js';

/** Thrown when input text is not valid JSON. */
export class JsonParseError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = 'JsonParseError';
  }
}

/** Parse JSON text into a canonical document. */
export function parse(text: string): Document {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch (err) {
    throw new JsonParseError(`invalid JSON: ${(err as Error).message}`, { cause: err });
  }
  return document(fromValue(value), { sourceFormat: 'json' });
}

/** Serialize a document or node to JSON text (2-space indent, trailing newline). */
export function serialize(target: Document | Node): string {
  const node = 'root' in target ? target.root : target;
  return `${JSON.stringify(toValue(node), null, 2)}\n`;
}
