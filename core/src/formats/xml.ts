/**
 * XML format pack.
 *
 * Like the JSON pack, this owns only the text⇄value boundary; the canonical
 * model owns value⇄node. XML is mapped with a flat convention so that XML
 * specifics never leak into transforms or paths:
 *
 * - attributes become `@`-prefixed fields (`order.@id`)
 * - element text becomes a `#text` field (`note.#text`)
 * - repeated elements become an array; a single element stays an object
 * - leaf values stay strings — XML is untyped, so no number/boolean coercion
 *
 * Known limitations: the single-vs-repeated ambiguity means one `<line/>`
 * round-trips as an object, not a one-element array; namespace prefixes are
 * kept verbatim as part of the key (no resolution); the XML declaration and
 * comments are dropped; `serialize` expects a single-root-element object, the
 * shape `parse` produces.
 */
import { XMLBuilder, XMLParser, XMLValidator } from 'fast-xml-parser';
import {
  type Document,
  type Node,
  type ValidationMessage,
  document,
  fromValue,
  toValue,
} from '../model.js';

const ATTR_PREFIX = '@';
const TEXT_NODE = '#text';

const parser = new XMLParser({
  ignoreAttributes: false,
  attributeNamePrefix: ATTR_PREFIX,
  textNodeName: TEXT_NODE,
  parseTagValue: false,
  parseAttributeValue: false,
  trimValues: true,
  ignoreDeclaration: true,
});

const builder = new XMLBuilder({
  ignoreAttributes: false,
  attributeNamePrefix: ATTR_PREFIX,
  textNodeName: TEXT_NODE,
  format: true,
  indentBy: '  ',
  suppressEmptyNode: true,
  // Keep attr="true" intact; the default renders it as a valueless attribute,
  // which is not well-formed XML and would break the round trip on reparse.
  suppressBooleanAttributes: false,
});

/** Thrown when input text is not well-formed XML. */
export class XmlParseError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = 'XmlParseError';
  }
}

/**
 * A pluggable XML validator. The default checks well-formedness; an XSD-backed
 * validator can implement this interface later without changing the pack.
 */
export interface XmlValidator {
  validate(text: string): ValidationMessage[];
}

/** Default validator: reports well-formedness errors, empty when valid. */
export const wellFormedValidator: XmlValidator = {
  validate(text: string): ValidationMessage[] {
    const result = XMLValidator.validate(text);
    if (result === true) return [];
    const { line, col, msg } = result.err;
    return [{ path: '', message: `line ${line}:${col}: ${msg}` }];
  },
};

/** Parse XML text into a canonical document. Throws on malformed input. */
export function parse(text: string, validator: XmlValidator = wellFormedValidator): Document {
  const errors = validator.validate(text);
  if (errors.length > 0) {
    throw new XmlParseError(`invalid XML: ${errors.map((e) => e.message).join('; ')}`);
  }
  const value = parser.parse(text) as unknown;
  return document(fromValue(value), { sourceFormat: 'xml' });
}

/** Serialize a document or node to XML text (2-space indent, trailing newline). */
export function serialize(target: Document | Node): string {
  const node = 'root' in target ? target.root : target;
  const text = builder.build(toValue(node)) as string;
  return text.endsWith('\n') ? text : `${text}\n`;
}
