# RFC 0001 — v0alpha2 Transform DSL

- Status: **Draft** (design only; no implementation yet)
- Target: after Milestone 8 (TypeScript escape hatch)
- Supersedes: the v0alpha1 op-keyed flow DSL (M7)
- Schema bump: `weavster/v0alpha1` → `weavster/v0alpha2` (breaking)

## Summary

Redesign the transform DSL around a small **expression language** (MongoDB-flavored),
while keeping the **mutate-in-place pipeline** so small edits stay small. Two sigils:
`$path` references read from the document, `_op` keys invoke operators (in any position —
as a step or as a value). Steps default to **patch** semantics (keep everything, change
only what you name); full reshape is an explicit opt-in.

## Motivation

v0alpha1 (M7) shipped an op-keyed pipeline: `map`, `rename`, `default`, `concat`, `str`,
`date`, `when`. Building it surfaced real limits:

1. **No composition.** Each op is a flat step. You cannot nest logic (e.g. uppercase the
   result of a concat) without intermediate fields. No path to reuse/macros.
2. **Inconsistent target keys** (`at` vs `to`), and `map` is a confusing name (reads like
   array-map; it actually copies one path to another).
3. **No unconditional literal set**, no `drop`/`select` — source fields leak into output.
4. **Noisy validation** — the step schema is a `oneOf` over op variants, so a bad `op`
   reports every branch's failure.

And one requirement v0alpha1 cannot meet well:

5. **Cheap partial edits.** For segment-heavy formats (e.g. HL7), a change is often "set
   `MSH-4`, reformat one date, append one patient id" — without re-emitting every segment
   and field. Pure projection (declare the whole output) is the opposite of this.

## Key decision: patch + reshape, not projection-only

MongoDB already reconciles "powerful expressions" with "cheap partial edits" by having two
modes. We adopt both:

- **Patch** (`_set` / `_default` / `_unset` / `_rename` / `_append`): keep the whole
  document, change only the named paths. **This is the default.**
- **Reshape** (`_select`): output only what you declare (projection). Explicit opt-in.

The backbone stays a **pipeline of steps** (so partial edits compose naturally and a step
trace remains debuggable). The new power is that **values are expressions**.

## Sigils

| Sigil  | Position     | Meaning                                  | Example              |
| ------ | ------------ | ---------------------------------------- | -------------------- |
| `$`    | scalar value | path reference into the working document | `$order.line[0].sku` |
| `_`    | map key      | operator invocation (step or value)      | `{ _concat: [...] }` |
| (none) | map key      | literal data / field name                | `status: new`        |

- One rule to remember: **`_` means "run an operator," `$` means "read a path," plain means
  "literal data."**
- Escapes: a literal string that must start with `$` → `$$...`, or wrap with `{ _lit: "$x" }`.
  A literal map that would look like an operator → `{ _lit: { ... } }`.
- Path references resolve against the **working document** (the state after prior steps),
  not the original input.

## Structural step operators

Each step is a single `_`-prefixed operator key. Dispatch is on that key (which also gives
clean validation — see below).

| Step       | Shape                                            | Semantics                                               |
| ---------- | ------------------------------------------------ | ------------------------------------------------------- |
| `_set`     | `{ <path>: <expr>, ... }`                        | Patch: set each path to its expr. Keep everything else. |
| `_default` | `{ <path>: <expr>, ... }`                        | Patch, but only where the path is currently absent.     |
| `_rename`  | `{ <from>: <to>, ... }`                          | Move each path (copy then remove source).               |
| `_unset`   | `[<path>, ...]`                                  | Remove paths.                                           |
| `_append`  | `{ to: <path>, value: <expr> }`                  | Append to an array (grows it).                          |
| `_select`  | `{ <path>: <expr>, ... }`                        | Reshape: output **only** these paths.                   |
| `_when`    | `{ cond: <expr>, then: [steps], else: [steps] }` | Run a branch (sub-pipeline) by condition.               |

`_set`, `_default`, and `_rename` take **maps** (many paths per step) for terse patches.

## Value operators (the expression language)

Used anywhere a value is expected — inside `_set`/`_select`/`_append`/`_default` and in
`_when.cond`. An expression is one of: a literal, a `$path` reference, or a single-key
`{ _op: args }` map.

| Operator                      | Shape                                             | Result                     |
| ----------------------------- | ------------------------------------------------- | -------------------------- |
| reference                     | `$a.b[0]`                                         | the value at that path     |
| `_concat`                     | `[<expr>, ...]` or `{ parts: [...], sep: <str> }` | joined string              |
| `_upper` / `_lower` / `_trim` | `<expr>`                                          | transformed string         |
| `_toIso`                      | `<expr>`                                          | date string → ISO-8601 UTC |
| `_coalesce`                   | `[<expr>, ...]`                                   | first non-null             |
| `_eq`                         | `[<expr>, <expr>]`                                | boolean                    |
| `_exists`                     | `<expr-path>`                                     | boolean (path present)     |
| `_cond`                       | `{ if: <expr>, then: <expr>, else: <expr> }`      | ternary, as a value        |

Future (not v0alpha2 unless cheap): `_gt`/`_lt`/`_in`, `_and`/`_or`/`_not`, numeric/math ops.

Note this **subsumes** several v0alpha1 ops: `map` is just `_set: { to: $from }`; `concat`,
`str.*`, `date.*` become value operators; `when`'s condition becomes an `_eq`/`_exists`
expression. The structural surface shrinks; the composable surface grows.

## Worked example — HL7-style partial edit

Set `MSH-4`, reformat a date in `PID-7`, append a patient id to the repeating `PID-3` —
leaving every other segment and field untouched:

```yaml
steps:
  - _set:
      MSH-4: SENDING_FAC
      PID-7: { _toIso: $PID-7 }
  - _append:
      to: PID-3
      value: MRN-12345
```

(The HL7 format pack itself is out of scope here; this shows the patch ergonomics the DSL
must support. Segment/field addressing maps onto the canonical model via that pack; the
existing dotted/bracket path syntax already covers arbitrary keys and array indices.)

## Worked example — reshape

```yaml
steps:
  - _select:
      id: { _upper: $order.@id }
      name: { _concat: { parts: [$first, $last], sep: ' ' } }
      priority:
        _cond:
          if: { _eq: [$status, new] }
          then: high
          else: normal
```

## Validation

Because each step is a single `_op` key, the schema dispatches on that key (e.g. JSON Schema
`if`/`then` per operator, or ajv `discriminator`), not a `oneOf` over an `op` constant. A bad
operator yields one clean message — "unknown operator `_st`" — instead of v0alpha1's
every-branch explosion. This resolves the M7 validation rough edge for free.

## Errors

Unchanged in spirit: a `TransformError` carries the step index, the operator, and the
offending path, and nests through `_when` branches. Reference to a missing path inside an
expression follows the same "missing → error" / "comparison of missing → false" rules as
v0alpha1.

## v0alpha1 → v0alpha2 migration

| v0alpha1                                          | v0alpha2                                               |
| ------------------------------------------------- | ------------------------------------------------------ |
| `op: map { from, to }`                            | `_set: { <to>: $<from> }`                              |
| `op: rename { from, to }`                         | `_rename: { <from>: <to> }`                            |
| `op: default { at, value }`                       | `_default: { <at>: <value> }`                          |
| `op: concat { to, parts, sep }`                   | `_set: { <to>: { _concat: { parts, sep } } }`          |
| `op: str { fn, from, to }`                        | `_set: { <to>: { _<fn>: $<from> } }`                   |
| `op: date { fn: toIso, from, to }`                | `_set: { <to>: { _toIso: $<from> } }`                  |
| `op: when { cond: { path, equals }, then, else }` | `_when: { cond: { _eq: [$<path>, <v>] }, then, else }` |
| (none) — set a literal always                     | `_set: { <path>: <value> }`                            |
| (none) — drop a field                             | `_unset: [<path>]`                                     |

Cleanup items from the M7 retro are absorbed: consistent target keys (maps remove `at`/`to`
split), unconditional set, drop/select, `map` renamed away, grouped string/date functions
become the expression namespace, validation noise fixed.

## Open questions

1. **Array ops** — is `_append` enough, or do we also need `_insert { to, index, value }`
   and removal by index? HL7 repeating fields will pressure this.
2. **Macros / reuse** — named, reusable expressions (e.g. a project-level `fullName`) are the
   real prize of the expression model. Design the seam in v0alpha2; ship in a later pass?
3. **`_select` semantics** — strict projection (only named paths) vs. deep-merge over a base.
   Proposed: strict.
4. **Literal escapes** — settle `_lit` vs `$$` for values that must start with `$`/look like
   operators.
5. **Numeric/logical operators** — how much of `_gt`/`_lt`/`_in`/`_and`/`_or`/`_not` lands in
   v0alpha2 vs later.
6. **Flow `apiVersion`** — do flow files declare `apiVersion: weavster/v0alpha2`, or is the
   version implied by the project?

## Non-goals

- Implementing this now (post-M8).
- A full Turing-complete expression language — that is what the TypeScript escape hatch (M8)
  is for. The "when not to use config" guidance still applies.
- The HL7 format pack itself (separate, later milestone).

## Sequencing

1. Finish M8 (TypeScript escape hatch) on the v0alpha1 DSL.
2. Resolve the open questions on this RFC.
3. Implement v0alpha2 in core (expression evaluator + structural ops), bump the schema,
   update the CLI loader, migrate the golden-path flow, rewrite the DSL docs.
