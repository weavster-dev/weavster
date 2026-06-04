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

  it('does not mutate the input document', () => {
    const input = docOf({ a: 1 });
    applyFlow(input, { steps: [{ op: 'map', from: 'a', to: 'b' }] });
    expect(toValue(input.root)).toEqual({ a: 1 });
  });

  it('throws on an unknown op', () => {
    expect(() => run({}, [{ op: 'frobnicate' }])).toThrow(TransformError);
  });
});
