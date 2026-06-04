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
