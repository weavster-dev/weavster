import { describe, expect, it } from 'vitest';
import { document, fromValue, toValue } from '../src/model.js';
import { type Flow, TransformError, applyFlow } from '../src/transform.js';

const docOf = (value: unknown) => document(fromValue(value), { sourceFormat: 'json' });
const run = (value: unknown, steps: Flow['steps']) =>
  toValue(applyFlow(docOf(value), { steps }).root);

describe('map', () => {
  it('copies a value to a new path', () => {
    expect(run({ order: { id: 'A-1' } }, [{ op: 'map', from: 'order.id', to: 'id' }])).toEqual({
      order: { id: 'A-1' },
      id: 'A-1',
    });
  });

  it('creates intermediate object segments on the target', () => {
    expect(run({ a: 1 }, [{ op: 'map', from: 'a', to: 'nested.deep.value' }])).toEqual({
      a: 1,
      nested: { deep: { value: 1 } },
    });
  });

  it('throws a step-scoped TransformError for a missing source', () => {
    expect(() => run({}, [{ op: 'map', from: 'missing', to: 'x' }])).toThrow(
      /step 0 \(map\).*missing/,
    );
  });
});

describe('rename', () => {
  it('moves a value: target set, source removed', () => {
    expect(
      run({ order: { '@id': 'A-1' } }, [{ op: 'rename', from: 'order.@id', to: 'order.id' }]),
    ).toEqual({ order: { id: 'A-1' } });
  });
});

describe('default', () => {
  it('fills a value when the path is absent', () => {
    expect(run({}, [{ op: 'default', at: 'status', value: 'new' }])).toEqual({ status: 'new' });
  });

  it('leaves an existing value untouched', () => {
    expect(run({ status: 'done' }, [{ op: 'default', at: 'status', value: 'new' }])).toEqual({
      status: 'done',
    });
  });

  it('accepts a structured default value', () => {
    expect(run({}, [{ op: 'default', at: 'meta', value: { tags: [] } }])).toEqual({
      meta: { tags: [] },
    });
  });
});

describe('concat', () => {
  it('joins paths and literals with a separator', () => {
    expect(
      run({ first: 'jane', last: 'doe' }, [
        { op: 'concat', to: 'name', sep: ' ', parts: [{ path: 'first' }, { path: 'last' }] },
      ]),
    ).toMatchObject({ name: 'jane doe' });
  });

  it('supports literal value parts and defaults the separator to empty', () => {
    expect(
      run({ area: '212', num: '5551234' }, [
        {
          op: 'concat',
          to: 'phone',
          parts: [{ value: '(' }, { path: 'area' }, { value: ') ' }, { path: 'num' }],
        },
      ]),
    ).toMatchObject({ phone: '(212) 5551234' });
  });

  it('errors on a missing source path', () => {
    expect(() => run({}, [{ op: 'concat', to: 'x', parts: [{ path: 'nope' }] }])).toThrow(
      /step 0 \(concat\).*nope/,
    );
  });

  it('errors when a part resolves to a non-scalar', () => {
    expect(() =>
      run({ obj: { a: 1 } }, [{ op: 'concat', to: 'x', parts: [{ path: 'obj' }] }]),
    ).toThrow(/must be a scalar/);
  });
});

describe('str', () => {
  it('applies upper/lower/trim', () => {
    expect(run({ v: 'aB' }, [{ op: 'str', fn: 'upper', from: 'v', to: 'v' }])).toEqual({ v: 'AB' });
    expect(run({ v: 'aB' }, [{ op: 'str', fn: 'lower', from: 'v', to: 'v' }])).toEqual({ v: 'ab' });
    expect(run({ v: '  x  ' }, [{ op: 'str', fn: 'trim', from: 'v', to: 'v' }])).toEqual({
      v: 'x',
    });
  });

  it('defaults the target to the source (in place)', () => {
    expect(run({ code: 'ab' }, [{ op: 'str', fn: 'upper', from: 'code' }])).toEqual({ code: 'AB' });
  });

  it('errors on an unknown fn', () => {
    expect(() => run({ v: 'x' }, [{ op: 'str', fn: 'reverse', from: 'v' }])).toThrow(
      /unknown str fn/,
    );
  });
});

describe('date', () => {
  it('converts a parseable date string to ISO 8601', () => {
    expect(run({ ts: '2026-06-04' }, [{ op: 'date', fn: 'toIso', from: 'ts' }])).toEqual({
      ts: '2026-06-04T00:00:00.000Z',
    });
  });

  it('errors on an unparseable date', () => {
    expect(() => run({ ts: 'not-a-date' }, [{ op: 'date', fn: 'toIso', from: 'ts' }])).toThrow(
      /cannot parse date/,
    );
  });
});

describe('when', () => {
  it('runs the then branch when equals matches', () => {
    expect(
      run({ status: 'new' }, [
        {
          op: 'when',
          cond: { path: 'status', equals: 'new' },
          then: [{ op: 'default', at: 'priority', value: 'high' }],
        },
      ]),
    ).toEqual({ status: 'new', priority: 'high' });
  });

  it('runs the else branch when the condition is false', () => {
    expect(
      run({ status: 'done' }, [
        {
          op: 'when',
          cond: { path: 'status', equals: 'new' },
          then: [{ op: 'default', at: 'priority', value: 'high' }],
          else: [{ op: 'default', at: 'priority', value: 'low' }],
        },
      ]),
    ).toEqual({ status: 'done', priority: 'low' });
  });

  it('does nothing when false and there is no else', () => {
    expect(
      run({ status: 'done' }, [
        {
          op: 'when',
          cond: { path: 'status', equals: 'new' },
          then: [{ op: 'default', at: 'priority', value: 'high' }],
        },
      ]),
    ).toEqual({ status: 'done' });
  });

  it('tests presence with exists', () => {
    expect(
      run({}, [
        {
          op: 'when',
          cond: { path: 'x', exists: false },
          then: [{ op: 'default', at: 'x', value: 1 }],
        },
      ]),
    ).toEqual({ x: 1 });
    expect(
      run({ x: 9 }, [
        {
          op: 'when',
          cond: { path: 'x', exists: true },
          then: [{ op: 'default', at: 'seen', value: true }],
        },
      ]),
    ).toEqual({ x: 9, seen: true });
  });

  it('treats equals against a missing or non-scalar value as false', () => {
    expect(
      run({}, [
        {
          op: 'when',
          cond: { path: 'k', equals: 'v' },
          then: [{ op: 'default', at: 'hit', value: 1 }],
        },
      ]),
    ).toEqual({});
  });

  it('reports nested step errors with the when context', () => {
    expect(() =>
      run({ a: 1 }, [
        {
          op: 'when',
          cond: { path: 'a', equals: 1 },
          then: [{ op: 'map', from: 'missing', to: 'y' }],
        },
      ]),
    ).toThrow(/step 0 \(when\): step 0 \(map\).*missing/);
  });

  it('errors on a malformed condition', () => {
    expect(() => run({}, [{ op: 'when', cond: { path: 'a' }, then: [] }])).toThrow(
      /equals.*exists/,
    );
  });
});

describe('ts', () => {
  const functions = {
    addName: (o: any) => ({ ...o, name: `${o.first} ${o.last}` }),
    up: (s: any) => String(s).toUpperCase(),
    boom: () => {
      throw new Error('boom');
    },
    withFn: () => ({ a: 1, f: () => 0 }),
  };
  const runTs = (value: unknown, steps: Flow['steps']) =>
    toValue(applyFlow(docOf(value), { steps }, { functions }).root);

  it('runs a function on the whole document by default', () => {
    expect(runTs({ first: 'jane', last: 'doe' }, [{ op: 'ts', module: 'addName' }])).toEqual({
      first: 'jane',
      last: 'doe',
      name: 'jane doe',
    });
  });

  it('runs a function on a from/to subpath', () => {
    expect(
      runTs({ code: 'ab', keep: 1 }, [{ op: 'ts', module: 'up', from: 'code', to: 'code' }]),
    ).toEqual({ code: 'AB', keep: 1 });
  });

  it('errors on a missing function', () => {
    expect(() => runTs({}, [{ op: 'ts', module: 'nope' }])).toThrow(
      /step 0 \(ts\).*no function "nope"/,
    );
  });

  it('wraps a thrown error with step context', () => {
    expect(() => runTs({}, [{ op: 'ts', module: 'boom' }])).toThrow(/step 0 \(ts\): boom/);
  });

  it('drops non-JSON output through the JSON boundary', () => {
    expect(runTs({}, [{ op: 'ts', module: 'withFn' }])).toEqual({ a: 1 });
  });
});

describe('applyFlow', () => {
  it('runs steps in order as a pipeline', () => {
    const flow: Flow = {
      steps: [
        { op: 'rename', from: 'order.@id', to: 'id' },
        { op: 'map', from: 'order.line', to: 'lines' },
        { op: 'default', at: 'status', value: 'new' },
      ],
    };
    expect(run({ order: { '@id': 'A-1', line: ['w', 'g'] } }, flow.steps)).toEqual({
      order: { line: ['w', 'g'] },
      id: 'A-1',
      lines: ['w', 'g'],
      status: 'new',
    });
  });

  it('runs a combined pipeline using helper ops', () => {
    expect(
      run({ first: 'jane', last: 'DOE', ts: '2026-06-04' }, [
        { op: 'str', fn: 'lower', from: 'last', to: 'last' },
        { op: 'concat', to: 'name', sep: ' ', parts: [{ path: 'first' }, { path: 'last' }] },
        { op: 'date', fn: 'toIso', from: 'ts', to: 'createdAt' },
        { op: 'default', at: 'status', value: 'new' },
      ]),
    ).toEqual({
      first: 'jane',
      last: 'doe',
      ts: '2026-06-04',
      name: 'jane doe',
      createdAt: '2026-06-04T00:00:00.000Z',
      status: 'new',
    });
  });

  it('does not mutate the input document', () => {
    const input = docOf({ a: 1 });
    applyFlow(input, { steps: [{ op: 'map', from: 'a', to: 'b' }] });
    expect(toValue(input.root)).toEqual({ a: 1 });
  });

  it('throws on an unknown op', () => {
    expect(() => run({}, [{ op: 'frobnicate' }])).toThrow(TransformError);
  });
});
