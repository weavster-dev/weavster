---
sidebar_position: 8
title: Pipelines
---

# Pipelines

A flow describes _how_ to transform a document; a **pipeline** describes _where the data comes
from and goes_. `weavster run` executes pipelines: read a **source**, transform with a **flow**,
write a **sink**.

Pipelines live one-per-file in `pipelines/`, alongside `flows/` and `fixtures/`:

```yaml
# pipelines/order.yaml
source:
  type: file
  path: in/order.json
flow: order # flows/order.yaml
sink:
  type: file
  path: out/order.json
```

```bash
weavster run order   # run pipelines/order.yaml
weavster run         # run every pipeline
```

## Connectors

A `source` and `sink` each have a `type`:

| Type     | As source          | As sink                     |
| -------- | ------------------ | --------------------------- |
| `file`   | read `path`        | write `path` (creates dirs) |
| `stdin`  | read process stdin | —                           |
| `stdout` | —                  | write process stdout        |

```yaml
source: { type: stdin, format: json }
flow: order
sink: { type: stdout }
```

(Network connectors such as REST and SFTP will land later on the same shape.)

## How `run` works

A source yields a **stream of documents**, and `run` processes each one as it arrives —
parse → flow → serialize → sink — staying live until the source closes:

- A **`file`** source is bounded: it yields the whole file as one document, then ends, so the
  loop runs once and `run` exits.
- A **`stdin`** source is unbounded and **line-delimited**: each non-empty line is one document,
  processed as it arrives. `run` blocks for the next line and exits at end-of-stream. (Pipe
  newline-delimited JSON: `cat orders.ndjson | weavster run orders`.)

This is the seam for always-on connectors later (REST/SFTP/queues) — same loop, unbounded source.

## Formats

The **source** format picks the parser, the **sink** format picks the serializer — so a
pipeline can convert formats (XML in, JSON out).

- **Source `file`** — inferred from the path extension (`.json`/`.xml`); set `format:` to
  override.
- **Source `stdin`** — `format:` is required (no extension to infer).
- **Sink** — defaults to the **source** format; a `file` sink with a recognized extension uses
  that; an explicit `format:` always wins.

A `file` sink overwrites its path. Converting to XML requires the document to have a single
root element (see the [Format Packs](./formats.md) limitations).

## Errors

Errors are split by when they happen:

- **Startup** (pipeline not found / schema-invalid, source fails to open) aborts the pipeline
  before the loop and exits non-zero.
- **Per-document** (parse, transform, or write failure for one document) is scoped to that
  document. On a bounded `file` source that single failure fails the run; on an unbounded
  stream it is logged and the loop continues to the next document.

`run` reports which pipeline, which document, and which stage failed.

## Validation

`weavster validate` checks every `pipelines/*.yaml` against the pipeline schema, alongside
`weavster.yaml` and your flows.
