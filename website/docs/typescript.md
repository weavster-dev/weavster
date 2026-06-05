---
sidebar_position: 7
title: TypeScript Transforms
---

# TypeScript transforms (escape hatch)

When the declarative [DSL](./dsl.md) cannot express a transform cleanly, drop to code with a
`ts` step. It runs a custom TypeScript function you write in your project.

> **Config first, TypeScript second.** Reach for a `ts` step only when the step list genuinely
> cannot express the logic — a non-trivial computation, branching the DSL can't model, or
> shaping that would be a long tail of fake-control-flow steps. Most transforms should stay
> declarative; they are easier to read, validate, and review.

## The contract: JSON in, JSON out

A custom function is a **pure function from a JSON value to a JSON value**:

```ts
// functions/initials.ts
interface Order {
  first?: string;
  last?: string;
  [key: string]: unknown;
}

export default (order: Order): Order => ({
  ...order,
  initials: `${order.first?.[0] ?? ''}${order.last?.[0] ?? ''}`.toUpperCase(),
});
```

- One **default export**, a function.
- Input and output are plain JSON (objects, arrays, scalars) — not Weavster's internal model.
- The result is taken through a JSON boundary, so anything non-JSON (functions, `undefined`)
  is dropped.

Keep it **pure JSON → JSON**. The local CLI runs your function as ordinary JavaScript, but the
production runtime executes transforms as WASM — a function that stays pure JSON in/out is
portable to both. Reaching for Node-only APIs works locally but will not port.

## The `ts` step

```yaml
steps:
  - op: ts
    module: initials # functions/initials.ts
    # from: order    # optional: operate on a subpath (default: whole document)
    # to: order      # optional: where to write (default: the root)
```

- `module` — the file `functions/<module>.ts` in your project; its default export is called.
- `from` — path to read and pass to the function. Omit for the whole document.
- `to` — path to write the result. Omit to replace the root.

## Errors

A missing function file, a module without a default-export function, or an error thrown
inside the function all surface as a `TransformError` naming the step and module:

```text
step 3 (ts): no function "initials" at .../functions/initials.ts
step 3 (ts): boom
```

The function is loaded on demand when `weavster test` runs the flow; no build step is needed.
