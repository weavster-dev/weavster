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

describe('_default', () => {
  it('fills only absent paths and leaves existing values', () => {
    expect(run({ a: 1 }, [{ _default: { a: 9, b: 2 } }])).toEqual({ a: 1, b: 2 });
  });
});

describe('_unset', () => {
  it('removes listed paths', () => {
    expect(run({ a: 1, b: 2, c: 3 }, [{ _unset: ['b', 'c'] }])).toEqual({ a: 1 });
  });
});

describe('_rename', () => {
  it('moves a path and skips missing sources', () => {
    expect(
      run({ order: { '@id': 'A1' } }, [{ _rename: { 'order.@id': 'id', 'order.nope': 'x' } }]),
    ).toEqual({ order: {}, id: 'A1' });
  });
});

describe('_append', () => {
  it('creates the array when absent', () => {
    expect(run({}, [{ _append: { to: 'ids', value: 'x' } }])).toEqual({ ids: ['x'] });
  });

  it('appends to an existing array', () => {
    expect(
      run({ ids: ['a'] }, [{ _append: { to: 'ids', value: '$seed' } }, { _set: { seed: 'b' } }]),
    ).toEqual({ ids: ['a', null], seed: 'b' });
  });

  it('errors when the target is not an array', () => {
    expect(() => run({ ids: 1 }, [{ _append: { to: 'ids', value: 'x' } }])).toThrow(/not an array/);
  });
});

describe('_select', () => {
  it('keeps only the named paths (strict projection)', () => {
    expect(
      run({ a: 1, b: 2, c: 3 }, [{ _select: { x: '$a', y: { _concat: ['$b', '-', '$c'] } } }]),
    ).toEqual({ x: 1, y: '2-3' });
  });

  it('skips a projected key whose value is undefined', () => {
    expect(run({ a: 1 }, [{ _select: { x: '$a', y: '$missing' } }])).toEqual({ x: 1 });
  });
});

describe('_when', () => {
  it('runs then/else by an expression condition', () => {
    const steps = (status: string) =>
      run({ status }, [
        {
          _when: {
            cond: { _eq: ['$status', 'new'] },
            then: [{ _set: { priority: 'high' } }],
            else: [{ _set: { priority: 'normal' } }],
          },
        },
      ]);
    expect(steps('new')).toEqual({ status: 'new', priority: 'high' });
    expect(steps('done')).toEqual({ status: 'done', priority: 'normal' });
  });

  it('reports nested step errors with the when context', () => {
    expect(() => run({}, [{ _when: { cond: true, then: [{ _frob: 1 }] } }])).toThrow(
      /step 0 \(_when\): step 0: unknown operator "_frob"/,
    );
  });
});

describe('_ts', () => {
  const functions = {
    addName: (o: any) => ({ ...o, name: `${o.first} ${o.last}` }),
    up: (s: any) => String(s).toUpperCase(),
    boom: () => {
      throw new Error('boom');
    },
  };
  const runTs = (value: unknown, steps: Flow['steps']) =>
    toValue(applyFlow(docOf(value), { steps }, { functions }).root);

  it('runs a function on the whole document by default', () => {
    expect(runTs({ first: 'jane', last: 'doe' }, [{ _ts: { module: 'addName' } }])).toEqual({
      first: 'jane',
      last: 'doe',
      name: 'jane doe',
    });
  });

  it('runs a function on a from/to subpath', () => {
    expect(
      runTs({ code: 'ab', keep: 1 }, [{ _ts: { module: 'up', from: 'code', to: 'code' } }]),
    ).toEqual({ code: 'AB', keep: 1 });
  });

  it('errors on a missing function and wraps a thrown error', () => {
    expect(() => runTs({}, [{ _ts: { module: 'nope' } }])).toThrow(
      /step 0 \(_ts\).*no function "nope"/,
    );
    expect(() => runTs({}, [{ _ts: { module: 'boom' } }])).toThrow(/step 0 \(_ts\): boom/);
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
