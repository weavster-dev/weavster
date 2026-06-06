# RFC 0002 — `weavster run` and pipelines

- Status: **Draft** (design only; no implementation yet)
- Phase: "make it move data" (first post-MVP phase)
- Builds on: the v0alpha2 DSL ([RFC 0001](./0001-v0alpha2-dsl.md)) and the existing format packs

## Summary

Add a config-driven `weavster run` that moves real data through a flow: read from a **source**,
transform with a **flow**, write to a **sink**. Pipelines are declared one-per-file in a
`pipelines/` directory (mirroring `flows/` and `fixtures/`). The first connectors are **local
file** and **stdin/stdout**.

## Motivation

Today Weavster transforms _fixtures_ — it proves a flow is correct but never touches real data.
The product is an _integration_ tool; the missing piece is actually moving a document from a
source, through a flow, to a sink. `run` is that piece.

## Pipelines

A pipeline is a file `pipelines/<name>.yaml`:

```yaml
# pipelines/orders.yaml
source:
  type: file
  path: in/order.json
flow: order # flows/order.yaml
sink:
  type: file
  path: out/order.json
```

Run it:

```bash
weavster run orders     # runs pipelines/orders.yaml
weavster run            # runs every pipeline in pipelines/
```

Data flow:

```text
source.read() ──▶ format pack parse ──▶ flow (applyFlow + functions) ──▶ format pack serialize ──▶ sink.write()
```

This reuses everything already built: the JSON/XML packs, the v0alpha2 engine, and the `_ts`
function loader. Only the source/sink I/O is new — and it lives in the CLI, keeping
`@weavster/core` pure.

## Connectors

A connector is a typed `source` or `sink`. The first two:

| Type     | As source          | As sink                     |
| -------- | ------------------ | --------------------------- |
| `file`   | read `path`        | write `path` (creates dirs) |
| `stdin`  | read process stdin | —                           |
| `stdout` | —                  | write process stdout        |

```yaml
source: { type: stdin, format: json }
flow: order
sink: { type: stdout, format: json }
```

The connector interface is small and pluggable, so REST/SFTP/etc. slot in later without
touching the run loop:

```ts
interface Source {
  read(): Promise<string>;
}
interface Sink {
  write(text: string): Promise<void>;
}
```

## Format selection

The **source** format chooses the parser; the **sink** format chooses the serializer — so a
pipeline can convert formats (e.g. XML in, JSON out).

- `file`: inferred from the path extension (`.json` → json, `.xml` → xml); override with an
  explicit `format:`.
- `stdin`/`stdout`: no extension, so `format:` is required (default `json` if omitted — TBD,
  see open questions).

## Validation

A new `spec/schemas/pipeline.schema.json` describes the pipeline file (source/flow/sink, known
connector types, valid format). `weavster validate` is extended to check every
`pipelines/*.yaml` too — alongside `weavster.yaml` and `flows/*.yaml`. The project's
`apiVersion` still governs; adding pipelines is additive and needs no schema-version bump.

## Errors

Each stage surfaces a clear, contextual error and a non-zero exit:

- pipeline not found / schema-invalid;
- source read failure (missing file, empty stdin);
- parse failure (`JsonParseError`/`XmlParseError`);
- transform failure (`TransformError`, already step-scoped);
- sink write failure (permissions, etc.).

`run` reports which pipeline and which stage failed.

## Slices

1. **Core run path + file/stdio connectors.** Pipeline schema + loader, the connector
   interface with `file`/`stdin`/`stdout`, the `weavster run [name]` command, `validate`
   extended to pipelines, and tests.
2. **Example + docs.** A golden-path pipeline (`pipelines/order.yaml` reading a sample input,
   writing output), a CLI Reference `run` section, a "Pipelines" docs page, and a CI smoke that
   runs the example end to end.

(File and stdio are both small; they may land together in slice 1.)

## Non-goals (this phase)

- Network connectors (REST, SFTP, TCP) — a later slice, on the same connector interface.
- Long-running / scheduled / watch execution — `run` is one-shot.
- The `compile` command — deferred until there's a concrete need (e.g. bundling for the
  Rust/WASM runtime).
- The Rust/WASM production runtime itself.
- Secrets management (only needed once network connectors arrive).

## Open questions

1. **stdio default format** — require `format:` explicitly, or default to `json`?
2. **`run` default target** — `run` with no name runs all pipelines (proposed); or should it
   require a name and treat "all" as `run --all`?
3. **File sink overwrite** — always overwrite, or refuse without `--force`?
4. **Multiple sources/sinks** — one each for now; is fan-in/fan-out ever needed here?
5. **Where does a pipeline's flow get its `_ts` functions** — same `functions/` dir as today
   (yes, proposed; the function loader is unchanged).
6. **Cross-format edge cases** — XML requires a single root element; a JSON→XML pipeline whose
   document isn't a single-root object errors at serialize. Document as a limitation.
