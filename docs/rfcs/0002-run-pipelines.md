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
weavster run            # runs every pipeline in pipelines/ (no flag needed)
```

`run` with no name **always** runs every pipeline in `pipelines/`. There is no `--all` flag —
running everything is the default, and naming a pipeline narrows to that one.

Data flow — the source yields a stream of documents and the run loop processes each one as it
arrives, staying live until the source closes:

```text
for each document from source:
  source ──▶ format pack parse ──▶ flow (applyFlow + functions) ──▶ format pack serialize ──▶ sink.write()
```

A bounded source (a `file`) yields one document, then closes — the loop runs once and `run`
exits. An unbounded source (`stdin`, and later REST/SFTP/queues) keeps yielding; `run` blocks
waiting for the next document and exits only when the source signals end-of-stream.

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
touching the run loop. A source is an async iterable of documents, so the same loop drives both
a one-shot file and an always-on stream:

```ts
interface Source {
  documents(): AsyncIterable<string>; // yields once for file; many for stdin/streams
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

Errors split by when they happen, because the loop is continuous:

**Startup errors** abort before the loop and exit non-zero:

- pipeline not found / schema-invalid;
- source open failure (missing file, unreachable endpoint).

**Per-document errors** are scoped to one document and, by default, do **not** kill a live
pipeline — the document is reported and the loop continues to the next:

- parse failure (`JsonParseError`/`XmlParseError`);
- transform failure (`TransformError`, already step-scoped);
- sink write failure (permissions, etc.).

End-of-stream is not an error: an empty/closed `stdin` or an exhausted `file` ends the loop
cleanly. For a bounded (one-shot) source, a per-document failure is the only document's failure,
so `run` exits non-zero; for an unbounded source it logs and keeps running. `run` reports which
pipeline, which document, and which stage failed.

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
- Scheduled / cron execution — out of scope for this phase. (`run` itself is **continuous**:
  like other ESBs, a pipeline stays live and keeps moving data from its source rather than
  exiting after one document. One-shot processing of a single fixed input is a degenerate case
  of the continuous loop, not the default.)
- The `compile` command — deferred until there's a concrete need (e.g. bundling for the
  Rust/WASM runtime).
- The Rust/WASM production runtime itself.
- Secrets management (only needed once network connectors arrive).

## Open questions

1. **stdio default format** — require `format:` explicitly, or default to `json`?
2. **File sink overwrite** — always overwrite, or refuse without `--force`?
3. **Multiple sources/sinks** — one each for now; is fan-in/fan-out ever needed here?
4. **Where does a pipeline's flow get its `_ts` functions** — same `functions/` dir as today
   (yes, proposed; the function loader is unchanged).
5. **Cross-format edge cases** — XML requires a single root element; a JSON→XML pipeline whose
   document isn't a single-root object errors at serialize. Document as a limitation.
