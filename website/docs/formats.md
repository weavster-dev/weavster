---
sidebar_position: 5
title: Format Packs
---

# Format packs

A format pack teaches Weavster how to read and write one wire format. It owns the
**text⇄value** boundary; the [canonical model](./concepts.md) owns **value⇄node**. So a
pack is thin: parse text to a native value and hand it to the model, or take a value back
from the model and render it as text.

```text
text ──parse──▶ value ──fromValue──▶ Node        (read)
Node ──toValue──▶ value ──serialize──▶ text       (write)
```

Because every pack targets the same canonical model, a transform written once applies
regardless of whether the input arrived as JSON or XML.

## JSON

The JSON pack is the first one, exposed under the `json` namespace of `@weavster/core`.

```ts
import { json, toValue } from '@weavster/core';

const doc = json.parse('{"orderId":"A-1","lines":[{"sku":"W","qty":3}]}');
doc.meta.sourceFormat; // 'json'
toValue(doc.root); // { orderId: 'A-1', lines: [{ sku: 'W', qty: 3 }] }

json.serialize(doc); // 2-space indented JSON with a trailing newline
```

- **`parse(text)`** runs `JSON.parse`, then normalizes the value into a canonical
  `Document` tagged `sourceFormat: 'json'`. Invalid input throws `JsonParseError`.
- **`serialize(docOrNode)`** converts back to a native value and renders it with a
  2-space indent and a trailing newline.

Serializing is stable — `serialize` applied to its own output returns the same text — so
JSON survives a parse→serialize→parse round trip with its values intact. The
[golden-path example](https://github.com/weavster-dev/weavster/tree/main/examples/golden-path)
exercises JSON fixtures end to end via `weavster test`.

## XML

The XML pack lives under the `xml` namespace of `@weavster/core` and is built on
[fast-xml-parser](https://github.com/NaturalIntelligence/fast-xml-parser). It maps XML
into the same object/array/scalar model so transforms never see XML-specific shapes:

| XML                  | Canonical                                       | Path             |
| -------------------- | ----------------------------------------------- | ---------------- |
| attribute `id="A-1"` | `@`-prefixed field                              | `order.@id`      |
| element text         | `#text` field (only when mixed with attributes) | `customer.#text` |
| text-only element    | a string                                        | `order.note`     |
| repeated elements    | an array                                        | `order.line[0]`  |

```ts
import { xml, toValue } from '@weavster/core';

const doc = xml.parse('<order id="A-1"><line>w</line><line>g</line></order>');
doc.meta.sourceFormat; // 'xml'
toValue(doc.root); // { order: { '@id': 'A-1', line: ['w', 'g'] } }

xml.serialize(doc); // indented XML with a trailing newline
```

- **`parse(text, validator?)`** validates well-formedness, then maps the document to a
  canonical `Document` tagged `sourceFormat: 'xml'`. Malformed input throws `XmlParseError`.
- **`serialize(docOrNode)`** renders the model back to indented XML.

### Validation

`parse` runs an `XmlValidator` first. The default `wellFormedValidator` only checks that
the input is well-formed XML. The interface is the seam for schema-aware validation later
(e.g. an XSD-backed validator) without changing the pack:

```ts
interface XmlValidator {
  validate(text: string): { path: string; message: string }[]; // empty = valid
}
```

### JSON vs XML, side by side

The same order in each format, with its normalized model:

```text
JSON                                XML
{ "@id": "A-1",                     <order id="A-1">
  "line": ["w", "g"] }                <line>w</line>
                                      <line>g</line>
                                    </order>

→ { '@id': 'A-1',                   → { order: { '@id': 'A-1',
    line: ['w', 'g'] }                            line: ['w', 'g'] } }
```

Within an element the two converge — attributes become `@`-fields and repeated children
become arrays, exactly the JSON shape. The one structural difference is the root: XML
always has a single root element, so its content sits one level down under that element's
name (`order.line[0]`), whereas JSON has no such wrapper (`line[0]`). A transform works on
either source once it addresses from the right root.

### Limitations

- **Single vs repeated**: one `<line/>` parses as an object, not a one-element array —
  XML cannot express "always a list", so a lone element is ambiguous.
- **Leaf typing**: all XML leaves are strings (`<qty>3</qty>` → `"3"`); there is no
  number/boolean coercion.
- **Namespaces**: prefixes are kept verbatim in the key (`ns:tag`); they are not resolved.
- **Dropped on parse**: the XML declaration and comments are not preserved.
- **Serialize input**: `serialize` expects a single-root-element object — the shape
  `parse` produces.
