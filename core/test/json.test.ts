import { describe, expect, it } from 'vitest';
import { toValue } from '../src/model.js';
import { JsonParseError, parse, serialize } from '../src/formats/json.js';

describe('parse', () => {
  it('parses JSON text into a canonical document tagged json', () => {
    const doc = parse('{"a": 1, "b": ["x"]}');
    expect(doc.meta.sourceFormat).toBe('json');
    expect(doc.root).toEqual({
      kind: 'object',
      fields: {
        a: { kind: 'scalar', value: 1 },
        b: { kind: 'array', items: [{ kind: 'scalar', value: 'x' }] },
      },
    });
  });

  it('parses top-level scalars and arrays', () => {
    expect(toValue(parse('42').root)).toBe(42);
    expect(toValue(parse('null').root)).toBeNull();
    expect(toValue(parse('[1, 2, 3]').root)).toEqual([1, 2, 3]);
  });

  it('throws JsonParseError on invalid JSON', () => {
    expect(() => parse('{not json}')).toThrow(JsonParseError);
  });
});

describe('serialize', () => {
  it('renders 2-space indented JSON with a trailing newline', () => {
    const out = serialize(parse('{"a":1}'));
    expect(out).toBe('{\n  "a": 1\n}\n');
  });

  it('accepts a bare node', () => {
    expect(serialize(parse('[1,2]').root)).toBe('[\n  1,\n  2\n]\n');
  });
});

describe('round-trip', () => {
  it('preserves values through parse → serialize → parse', () => {
    const text = serialize(
      parse('{"orderId":"A-1","lines":[{"sku":"W","qty":3}],"active":false,"note":null}'),
    );
    const first = toValue(parse(text).root);
    const second = toValue(parse(serialize(parse(text))).root);
    expect(first).toEqual({
      orderId: 'A-1',
      lines: [{ sku: 'W', qty: 3 }],
      active: false,
      note: null,
    });
    expect(second).toEqual(first);
  });

  it('is stable: serialize is idempotent on its own output', () => {
    const once = serialize(parse('{"b":2,"a":1}'));
    expect(serialize(parse(once))).toBe(once);
  });
});
