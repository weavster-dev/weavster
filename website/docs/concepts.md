---
sidebar_position: 2
title: Concepts
---

# Concepts

Core ideas: config-first authoring, the canonical document model, format packs, and transforms.

## The canonical document model

Weavster never transforms raw JSON or XML directly. Every input is first **normalized**
into one internal shape — the canonical document model — and transforms operate on that.
This is why a single transform can later target inputs that arrived as JSON _or_ XML: by
the time a transform runs, the format is gone and only the canonical model remains.

### Nodes

A `Node` is a tagged union of three structural kinds:

| Kind     | Shape                        | Represents                                    |
| -------- | ---------------------------- | --------------------------------------------- |
| `scalar` | `{ kind: 'scalar', value }`  | a leaf: string, number, boolean, or null      |
| `object` | `{ kind: 'object', fields }` | a keyed map; key insertion order is preserved |
| `array`  | `{ kind: 'array', items }`   | an ordered list                               |

A `Document` wraps a root node with metadata:

```ts
interface Document {
  root: Node;
  meta: {
    sourceFormat: 'json' | 'xml' | 'unknown';
    errors: { path: string; message: string }[];
  };
}
```

`sourceFormat` records where the document came from; `errors` collects validation
messages keyed by path. The structural kinds stay the same no matter the source format —
a future XML pack maps elements to objects, attributes to fields, and repeated elements
to arrays, all targeting the same three kinds.

### Normalization

Format packs turn text into native JS values; the model's `fromValue` then turns a value
into nodes, and `toValue` reverses it. Tracing `{ "orderId": "A-1", "lines": [{ "sku": "W" }] }`:

```text
object
├─ orderId: scalar "A-1"
└─ lines:   array
            └─ [0]: object
                    └─ sku: scalar "W"
```

### Paths

Nodes are addressed by **path**. The canonical form is a segment array (string segments
address object fields, numbers address array indices); the string form is dotted with
bracketed indices:

```text
lines[0].sku   ⇄   ['lines', 0, 'sku']
```

`get` resolves a path to a node (or `undefined` if any segment is absent) and `getValue`
resolves it to a native value. A numeric object key stays a string (`counts.0`), while an
array index uses brackets (`counts[0]`), so the two never collide. This path syntax is
what the transform DSL will accept from authors.
