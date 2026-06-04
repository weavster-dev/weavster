import { describe, expect, it } from 'vitest';
import { document, fromValue } from '../src/model.js';
import { formatPath, get, getValue, parsePath } from '../src/path.js';

describe('parsePath', () => {
  it('parses the root as empty', () => {
    expect(parsePath('')).toEqual([]);
  });

  it('parses dotted keys and bracket indices', () => {
    expect(parsePath('lines[0].sku')).toEqual(['lines', 0, 'sku']);
    expect(parsePath('a.b.c')).toEqual(['a', 'b', 'c']);
    expect(parsePath('matrix[1][2]')).toEqual(['matrix', 1, 2]);
  });

  it('keeps a numeric object key as a string', () => {
    expect(parsePath('counts.0')).toEqual(['counts', '0']);
  });
});

describe('formatPath', () => {
  it('is the inverse of parsePath', () => {
    for (const path of ['lines[0].sku', 'a.b.c', 'matrix[1][2]', 'counts.0']) {
      expect(formatPath(parsePath(path))).toBe(path);
    }
  });
});

describe('get', () => {
  const doc = document(fromValue({ orderId: 'A-1', lines: [{ sku: 'W', qty: 3 }, { sku: 'G' }] }));

  it('resolves a nested object field', () => {
    expect(get(doc, 'orderId')).toEqual({ kind: 'scalar', value: 'A-1' });
  });

  it('resolves through an array index', () => {
    expect(getValue(doc, 'lines[1].sku')).toBe('G');
  });

  it('accepts a segment array directly', () => {
    expect(getValue(doc, ['lines', 0, 'qty'])).toBe(3);
  });

  it('returns undefined for a missing field', () => {
    expect(get(doc, 'missing')).toBeUndefined();
    expect(get(doc, 'lines[0].missing')).toBeUndefined();
  });

  it('returns undefined for an out-of-range index', () => {
    expect(get(doc, 'lines[5]')).toBeUndefined();
  });

  it('returns undefined when indexing a non-array or keying a non-object', () => {
    expect(get(doc, 'orderId[0]')).toBeUndefined();
    expect(get(doc, 'orderId.x')).toBeUndefined();
  });

  it('resolves the same path regardless of how the document was built', () => {
    // The model is format-agnostic: a document a future JSON pack would build and
    // one a future XML pack would build resolve a shared path identically.
    const fromJsonLike = document(fromValue({ item: { id: 7 } }), { sourceFormat: 'json' });
    const fromXmlLike = document(fromValue({ item: { id: 7 } }), { sourceFormat: 'xml' });
    expect(getValue(fromJsonLike, 'item.id')).toBe(getValue(fromXmlLike, 'item.id'));
  });
});
