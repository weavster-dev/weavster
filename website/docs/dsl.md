---
sidebar_position: 6
title: Transform DSL
---

# Transform DSL

A **flow** transforms a document with a list of declarative steps — no code. Steps run in
order as a **mutate-in-place pipeline**: the input document is cloned into a working
document, each step edits it, and the result is the transformed document. Steps operate on
the [canonical model](./concepts.md) and address values by [path](./concepts.md#paths), so
the same flow runs whether the input arrived as JSON or XML.

```yaml
# flows/order.yaml
steps:
  - op: rename
    from: order.@id
    to: id
  - op: map
    from: order.line
    to: lines
  - op: default
    at: status
    value: new
```

Each step is **op-keyed**: the `op` field names the operation, and the remaining fields are
its arguments. Paths use the dotted/bracket form (`order.line[0].sku`).

## Operations

### `map`

Copy the value at `from` to `to`. Missing object segments in `to` are created.

```yaml
- op: map
  from: order.id
  to: id
```

### `rename`

Move the value at `from` to `to` — copy, then remove the source.

```yaml
- op: rename
  from: order.@id
  to: id
```

### `default`

Set `at` to `value` only when nothing is there yet. An existing value is left untouched.
`value` can be any literal — scalar, object, or array.

```yaml
- op: default
  at: status
  value: new
```

### `concat`

Join `parts` into a string at `to`. Each part is either a `path` (read from the document)
or a literal `value`. An optional `sep` is placed between parts (default empty). Parts must
resolve to scalars; `null` contributes an empty string.

```yaml
- op: concat
  to: fullName
  sep: ' '
  parts:
    - path: first
    - path: last
    - value: '!'
```

### `str`

Apply a string function — `upper`, `lower`, or `trim` — from `from` to `to`. `to` defaults
to `from`, so omitting it transforms in place.

```yaml
- op: str
  fn: upper
  from: code # writes back to `code`
```

### `date`

Apply a date function from `from` to `to` (default `from`). `toIso` parses the source value
as a date and writes it back as an ISO-8601 UTC string; an unparseable value is an error.

```yaml
- op: date
  fn: toIso
  from: createdAt # '2026-06-04' -> '2026-06-04T00:00:00.000Z'
```

## Errors

A step that references a missing source path, or that targets an impossible location (for
example writing through an array index that does not exist), fails with a `TransformError`
naming the step index and op:

```text
step 1 (map): no value at "order.missing"
```

The flow stops at the first failing step — later steps do not run.

## When not to use config

The DSL is for straightforward field-level reshaping. Reach for the (future) TypeScript
escape hatch instead when a transform needs real logic — lookups against external data,
non-trivial branching, or computation the step list cannot express clearly. If a flow grows
a long tail of conditional steps to fake control flow, that is the signal to drop to code.
