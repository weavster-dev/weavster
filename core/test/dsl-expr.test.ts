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

describe('value operators', () => {
  const ctx = ctxOf({ first: 'jane', last: 'doe', n: 5, tags: ['a', 'b'], when: '2026-06-04' });

  it('_concat joins, in array or { parts, sep } form', () => {
    expect(evalExpr({ _concat: ['$first', '!'] }, ctx)).toBe('jane!');
    expect(evalExpr({ _concat: { parts: ['$first', '$last'], sep: ' ' } }, ctx)).toBe('jane doe');
  });

  it('_upper / _lower / _trim', () => {
    expect(evalExpr({ _upper: '$first' }, ctx)).toBe('JANE');
    expect(evalExpr({ _lower: 'AB' }, ctx)).toBe('ab');
    expect(evalExpr({ _trim: '  x ' }, ctx)).toBe('x');
  });

  it('_toIso converts a date, or throws on an unparseable one', () => {
    expect(evalExpr({ _toIso: '$when' }, ctx)).toBe('2026-06-04T00:00:00.000Z');
    expect(() => evalExpr({ _toIso: 'nope' }, ctx)).toThrow(TransformError);
  });

  it('_coalesce returns the first non-null', () => {
    expect(evalExpr({ _coalesce: ['$missing', '$first'] }, ctx)).toBe('jane');
    expect(evalExpr({ _coalesce: ['$missing', null] }, ctx)).toBeNull();
  });

  it('_eq and _exists', () => {
    expect(evalExpr({ _eq: ['$first', 'jane'] }, ctx)).toBe(true);
    expect(evalExpr({ _eq: ['$first', 'x'] }, ctx)).toBe(false);
    expect(evalExpr({ _exists: '$first' }, ctx)).toBe(true);
    expect(evalExpr({ _exists: '$missing' }, ctx)).toBe(false);
  });

  it('_gt / _lt / _in', () => {
    expect(evalExpr({ _gt: ['$n', 3] }, ctx)).toBe(true);
    expect(evalExpr({ _lt: ['$n', 3] }, ctx)).toBe(false);
    expect(evalExpr({ _in: ['$last', ['doe', 'roe']] }, ctx)).toBe(true);
    expect(evalExpr({ _in: ['b', '$tags'] }, ctx)).toBe(true);
  });

  it('_and / _or / _not', () => {
    expect(evalExpr({ _and: [{ _eq: ['$first', 'jane'] }, { _gt: ['$n', 1] }] }, ctx)).toBe(true);
    expect(evalExpr({ _or: [{ _eq: ['$first', 'x'] }, { _gt: ['$n', 1] }] }, ctx)).toBe(true);
    expect(evalExpr({ _not: { _eq: ['$first', 'x'] } }, ctx)).toBe(true);
  });

  it('_cond chooses a branch and evaluates only the taken one', () => {
    expect(evalExpr({ _cond: { if: { _gt: ['$n', 3] }, then: 'big', else: 'small' } }, ctx)).toBe(
      'big',
    );
    expect(evalExpr({ _cond: { if: { _lt: ['$n', 3] }, then: 'big', else: 'small' } }, ctx)).toBe(
      'small',
    );
  });
});
