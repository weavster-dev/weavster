import { describe, expect, it } from 'vitest';
import { toValue } from '../src/model.js';
import {
  XmlParseError,
  type XmlValidator,
  parse,
  serialize,
  wellFormedValidator,
} from '../src/formats/xml.js';

describe('parse', () => {
  it('tags the document as xml', () => {
    expect(parse('<a>x</a>').meta.sourceFormat).toBe('xml');
  });

  it('maps a text-only element to a string', () => {
    expect(toValue(parse('<order><note>hi</note></order>').root)).toEqual({
      order: { note: 'hi' },
    });
  });

  it('maps attributes to @-prefixed fields and mixed text to #text', () => {
    expect(
      toValue(parse('<order id="A-1"><customer vip="true">acme</customer></order>').root),
    ).toEqual({
      order: {
        '@id': 'A-1',
        customer: { '#text': 'acme', '@vip': 'true' },
      },
    });
  });

  it('maps repeated elements to an array', () => {
    expect(toValue(parse('<order><line>w</line><line>g</line></order>').root)).toEqual({
      order: { line: ['w', 'g'] },
    });
  });

  it('keeps leaf values as strings (no number/boolean coercion)', () => {
    expect(toValue(parse('<order><qty>3</qty><paid>true</paid></order>').root)).toEqual({
      order: { qty: '3', paid: 'true' },
    });
  });

  it('throws XmlParseError on malformed XML', () => {
    expect(() => parse('<a><b></a>')).toThrow(XmlParseError);
  });
});

describe('serialize', () => {
  it('renders indented XML with a trailing newline', () => {
    expect(serialize(parse('<order id="A-1"><line>w</line></order>'))).toBe(
      '<order id="A-1">\n  <line>w</line>\n</order>\n',
    );
  });
});

describe('round-trip', () => {
  it('preserves structure through parse → serialize → parse', () => {
    const xml =
      '<order id="A-1"><customer vip="true">acme</customer><line>w</line><line>g</line></order>';
    const once = parse(xml);
    const twice = parse(serialize(once));
    expect(toValue(twice.root)).toEqual(toValue(once.root));
  });

  it('is stable: serialize is idempotent on its own output', () => {
    const once = serialize(parse('<order id="A-1"><line>w</line></order>'));
    expect(serialize(parse(once))).toBe(once);
  });
});

describe('wellFormedValidator', () => {
  it('returns no messages for valid XML', () => {
    expect(wellFormedValidator.validate('<a>x</a>')).toEqual([]);
  });

  it('reports a message for malformed XML', () => {
    const errors = wellFormedValidator.validate('<a><b></a>');
    expect(errors.length).toBeGreaterThan(0);
    expect(errors[0].message).toMatch(/line \d+:\d+:/);
  });

  it('lets a custom validator gate parsing', () => {
    const rejectAll: XmlValidator = {
      validate: () => [{ path: '', message: 'rejected by policy' }],
    };
    expect(() => parse('<a>x</a>', rejectAll)).toThrow(/rejected by policy/);
  });
});
