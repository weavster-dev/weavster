import { describe, expect, it } from 'vitest';
import { document, fromValue } from '../src/model.js';
import { TransformError } from '../src/dsl/errors.js';
import { type Ctx, evalExpr } from '../src/dsl/expr.js';

const ctxOf = (value: unknown): Ctx => ({
  working: document(fromValue(value), { sourceFormat: 'json' }),
});

describe('evalExpr', () => {
  it('returns literals unchanged', () => {
    const ctx = ctxOf({});
    expect(evalExpr('hi', ctx)).toBe('hi');
    expect(evalExpr(42, ctx)).toBe(42);
    expect(evalExpr(true, ctx)).toBe(true);
    expect(evalExpr(null, ctx)).toBeNull();
  });

  it('resolves $path references against the working document', () => {
    const ctx = ctxOf({ order: { id: 'A-1', lines: [{ sku: 'W' }] } });
    expect(evalExpr('$order.id', ctx)).toBe('A-1');
    expect(evalExpr('$order.lines[0].sku', ctx)).toBe('W');
  });

  it('returns undefined for a missing reference', () => {
    expect(evalExpr('$nope.missing', ctxOf({}))).toBeUndefined();
  });

  it('treats $$ as an escaped literal dollar string', () => {
    expect(evalExpr('$$order.id', ctxOf({}))).toBe('$order.id');
  });

  it('evaluates arrays and plain objects deeply', () => {
    const ctx = ctxOf({ a: 1, b: 2 });
    expect(evalExpr(['x', '$a', { n: '$b' }], ctx)).toEqual(['x', 1, { n: 2 }]);
  });

  it('returns _lit argument verbatim (escape hatch)', () => {
    const ctx = ctxOf({ a: 1 });
    expect(evalExpr({ _lit: '$a' }, ctx)).toBe('$a');
    expect(evalExpr({ _lit: { _set: 'not-an-op' } }, ctx)).toEqual({ _set: 'not-an-op' });
  });

  it('throws on an unknown operator', () => {
    expect(() => evalExpr({ _nope: 1 }, ctxOf({}))).toThrow(TransformError);
  });
});
