import { describe, expect, it } from 'vitest';
import { document, fromValue, toValue } from '../src/model.js';
import { type Flow, TransformError, applyFlow } from '../src/dsl/engine.js';

const docOf = (value: unknown) => document(fromValue(value), { sourceFormat: 'json' });
const run = (value: unknown, steps: Flow['steps']) =>
  toValue(applyFlow(docOf(value), { steps }).root);

describe('_set', () => {
  it('patches named paths and keeps the rest of the document', () => {
    expect(run({ a: 1, b: 2 }, [{ _set: { c: 3 } }])).toEqual({ a: 1, b: 2, c: 3 });
  });

  it('sets from a $path reference', () => {
    expect(run({ order: { id: 'A-1' } }, [{ _set: { id: '$order.id' } }])).toEqual({
      order: { id: 'A-1' },
      id: 'A-1',
    });
  });

  it('creates intermediate object segments', () => {
    expect(run({}, [{ _set: { 'a.b.c': 1 } }])).toEqual({ a: { b: { c: 1 } } });
  });

  it('builds a nested value with embedded references', () => {
    expect(run({ a: 1, b: 2 }, [{ _set: { out: { x: '$a', y: ['$b', 'lit'] } } }])).toEqual({
      a: 1,
      b: 2,
      out: { x: 1, y: [2, 'lit'] },
    });
  });

  it('skips a key whose value resolves to undefined (missing reference)', () => {
    expect(run({ a: 1 }, [{ _set: { b: '$missing' } }])).toEqual({ a: 1 });
  });

  it('writes an explicit null literal', () => {
    expect(run({ a: 1 }, [{ _set: { b: null } }])).toEqual({ a: 1, b: null });
  });

  it('evaluates sibling keys independently (no intra-step ordering)', () => {
    // `b` reads the original `a`, not a value written earlier in the same step.
    expect(run({ a: 1 }, [{ _set: { a: 2, b: '$a' } }])).toEqual({ a: 2, b: 1 });
  });
});

describe('_unset', () => {
  it('removes listed paths', () => {
    expect(run({ a: 1, b: 2, c: 3 }, [{ _unset: ['b', 'c'] }])).toEqual({ a: 1 });
  });
});

describe('applyFlow', () => {
  it('does not mutate the input document', () => {
    const input = docOf({ a: 1 });
    applyFlow(input, { steps: [{ _set: { b: 2 } }] });
    expect(toValue(input.root)).toEqual({ a: 1 });
  });

  it('throws on a step without exactly one operator key', () => {
    expect(() => run({}, [{ _set: {}, _unset: [] }])).toThrow(/exactly one operator key/);
  });

  it('throws on an unknown operator with step context', () => {
    expect(() => run({}, [{ _frobnicate: 1 }])).toThrow(/step 0: unknown operator "_frobnicate"/);
  });
});
