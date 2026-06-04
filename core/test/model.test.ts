import { describe, expect, it } from 'vitest';
import { document, fromValue, isArray, isObject, isScalar, toValue } from '../src/model.js';

describe('fromValue', () => {
  it('normalizes scalars', () => {
    expect(fromValue('hi')).toEqual({ kind: 'scalar', value: 'hi' });
    expect(fromValue(3)).toEqual({ kind: 'scalar', value: 3 });
    expect(fromValue(true)).toEqual({ kind: 'scalar', value: true });
    expect(fromValue(null)).toEqual({ kind: 'scalar', value: null });
  });

  it('treats undefined as a null scalar', () => {
    expect(fromValue(undefined)).toEqual({ kind: 'scalar', value: null });
  });

  it('normalizes nested objects and arrays', () => {
    const node = fromValue({ a: 1, b: [{ c: 'x' }] });
    expect(node).toEqual({
      kind: 'object',
      fields: {
        a: { kind: 'scalar', value: 1 },
        b: {
          kind: 'array',
          items: [{ kind: 'object', fields: { c: { kind: 'scalar', value: 'x' } } }],
        },
      },
    });
  });

  it('preserves object key insertion order', () => {
    const node = fromValue({ z: 1, a: 2, m: 3 });
    expect(isObject(node) && Object.keys(node.fields)).toEqual(['z', 'a', 'm']);
  });
});

describe('toValue', () => {
  it('round-trips arbitrary nested values', () => {
    const value = { id: 'A-1', lines: [{ sku: 'W', qty: 3 }], active: false, note: null };
    expect(toValue(fromValue(value))).toEqual(value);
  });
});

describe('guards', () => {
  it('discriminate node kinds', () => {
    expect(isScalar(fromValue(1))).toBe(true);
    expect(isObject(fromValue({}))).toBe(true);
    expect(isArray(fromValue([]))).toBe(true);
  });
});

describe('document', () => {
  it('defaults metadata', () => {
    const doc = document(fromValue({}));
    expect(doc.meta).toEqual({ sourceFormat: 'unknown', errors: [] });
  });

  it('carries source format and errors', () => {
    const doc = document(fromValue({}), {
      sourceFormat: 'json',
      errors: [{ path: 'a', message: 'bad' }],
    });
    expect(doc.meta.sourceFormat).toBe('json');
    expect(doc.meta.errors).toEqual([{ path: 'a', message: 'bad' }]);
  });
});
