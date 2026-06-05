---
sidebar_position: 9
title: Architecture
---

# Architecture

Weavster is config-first: you describe transformations in YAML, and a small engine runs them
over a single internal representation. The pipeline has clear stages, each replaceable.

```text
input text ──▶ format pack ──▶ canonical model ──▶ flow engine ──▶ format pack ──▶ output text
   (JSON/XML)     parse           Document            steps             serialize
```

## Stages

1. **Format pack** — owns the wire format. It parses text into a native value and serializes
   a value back to text; it never sees transforms. See [Format Packs](./formats.md). JSON and
   XML ship today.
2. **Canonical model** — one format-agnostic shape (`scalar` / `object` / `array`) that every
   input normalizes into, addressed by [paths](./concepts.md#paths). Because transforms only
   ever see this model, a flow written once works whether the source was JSON or XML. See
   [Concepts](./concepts.md).
3. **Flow engine** — runs a flow's steps as a patch-by-default pipeline, with expression
   values. See the [Transform DSL](./dsl.md).
4. **Escape hatch** — when the declarative DSL isn't enough, a `_ts` step runs a custom
   function under a pure JSON-in/JSON-out contract. See [TypeScript Transforms](./typescript.md).

## Packages

| Package          | Responsibility                                                       |
| ---------------- | -------------------------------------------------------------------- |
| `@weavster/core` | canonical model, path access, format packs, and the transform engine |
| `@weavster/cli`  | the `weavster` command: `init`, `validate`, `test` (loads + runs)    |

`core` is pure and has no filesystem or CLI dependencies — it takes parsed flows and injected
functions. The CLI owns project loading (config, flows, and custom functions via jiti) and
hands them to `core`. This keeps the engine unit-testable and portable.

## Local vs production

Today everything runs locally through the TypeScript CLI. The intended production runtime is a
**Rust server that executes transforms as WASM**. That is why the escape-hatch contract is
strictly JSON-in/JSON-out and the DSL semantics are defined precisely rather than as "whatever
JavaScript does": a transform authored locally is meant to stay portable to the WASM runtime.
The production runtime itself is future work, not part of the MVP.

## Boundaries the design holds

- Format specifics never leak past the format pack into transforms.
- The engine never reaches into the filesystem; the CLI injects everything it needs.
- Custom code is a pure function at a JSON boundary, not an open door into the host.
