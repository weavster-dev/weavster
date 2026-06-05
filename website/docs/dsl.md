---
sidebar_position: 6
title: Transform DSL
---

# Transform DSL

A **flow** transforms a document with a list of declarative steps — no code. Steps run in
order as a **patch-by-default pipeline**: each step changes only what it names and leaves the
rest of the document intact. Values are **expressions**, so you can read fields, compose, and
branch inline.

```yaml
# flows/order.yaml
steps:
  - _set:
      id: { _upper: $id }
      name: { _concat: { parts: [$first, $last], sep: ' ' } }
  - _when:
      cond: { _eq: [$status, new] }
      then:
        - _set: { priority: high }
      else:
        - _set: { priority: normal }
```

## Sigils

One rule: **`_` runs an operator, `$` reads a path, plain is literal data.**

| Sigil  | Where     | Meaning                                            | Example               |
| ------ | --------- | -------------------------------------------------- | --------------------- |
| `$`    | a value   | read a [path](./concepts.md#paths) from the doc    | `$order.line[0].sku`  |
| `_`    | a map key | invoke an operator (a step, or inside a value)     | `{ _concat: [...] }`  |
| `$$`   | a value   | an escaped literal `$` (so `$$x` is the text `$x`) | `$$rate`              |
| `_lit` | a value   | return the argument verbatim (escape an operator)  | `{ _lit: { _x: 1 } }` |
| (none) | a map key | a literal field name / data                        | `status: new`         |

Plain arrays and objects are evaluated deeply, so references and operators nest anywhere
inside a value. A `$path` that doesn't exist evaluates to nothing (missing), which `_set`
skips — so copying an optional field is a no-op, not a null write.

## Structural steps

Each step is exactly one `_`-prefixed operator. They are **patch** operators (keep the rest
of the document) except `_select`, which reshapes.

| Step       | Shape                                            | Does                                            |
| ---------- | ------------------------------------------------ | ----------------------------------------------- |
| `_set`     | `{ <path>: <expr>, ... }`                        | set each path; keep everything else             |
| `_default` | `{ <path>: <expr>, ... }`                        | set each path only where it is currently absent |
| `_unset`   | `[<path>, ...]`                                  | remove paths                                    |
| `_rename`  | `{ <from>: <to>, ... }`                          | move paths (missing sources are skipped)        |
| `_append`  | `{ to: <path>, value: <expr> }`                  | append to an array (created if absent)          |
| `_select`  | `{ <path>: <expr>, ... }`                        | **reshape**: output only the named paths        |
| `_when`    | `{ cond: <expr>, then: [steps], else: [steps] }` | run a branch by condition (`else` optional)     |
| `_ts`      | `{ module, from?, to? }`                         | run a [custom function](./typescript.md)        |

`_set`/`_default`/`_rename` take maps (many paths per step). `_set` evaluates all its values
against the document as it was at the start of the step, so sibling keys are independent.

## Value operators

Usable anywhere a value is expected (inside `_set`/`_select`/`_append`/… and in `_when.cond`).

| Operator                      | Shape                                        | Result                        |
| ----------------------------- | -------------------------------------------- | ----------------------------- |
| reference                     | `$a.b[0]`                                    | the value at that path        |
| `_concat`                     | `[<expr>, ...]` or `{ parts, sep }`          | joined string                 |
| `_upper` / `_lower` / `_trim` | `<expr>`                                     | transformed string            |
| `_toIso`                      | `<expr>`                                     | date string → ISO-8601 UTC    |
| `_coalesce`                   | `[<expr>, ...]`                              | first non-null                |
| `_eq` / `_gt` / `_lt`         | `[<expr>, <expr>]`                           | boolean comparison            |
| `_in`                         | `[<needle>, <arrayExpr>]`                    | membership boolean            |
| `_exists`                     | `<expr>`                                     | true if the value is present  |
| `_and` / `_or`                | `[<expr>, ...]`                              | boolean over the list         |
| `_not`                        | `<expr>`                                     | boolean negation              |
| `_cond`                       | `{ if: <expr>, then: <expr>, else: <expr> }` | a value chosen by a condition |

Operators compose: `{ _cond: { if: { _gt: [$total, 100] }, then: gold, else: standard } }`.

## Errors

A malformed step or a bad reference fails with a `TransformError` naming the step and
operator; nested `_when` branches carry their context:

```text
step 1 (_set): "_concat" expects a list or { parts, sep }
step 0 (_when): step 0 (_ts): no function "enrich"
```

## When not to use config

The DSL is for declarative reshaping. Reach for the
[TypeScript escape hatch](./typescript.md) (`_ts`) when a transform needs real logic the
operators can't express clearly. If a flow grows a long tail of `_when` steps faking control
flow, that is the signal to drop to code.
