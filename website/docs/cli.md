---
sidebar_position: 3
title: CLI Reference
---

# CLI Reference

The `weavster` CLI runs against a project directory containing a `weavster.yaml`.

The planned commands are `init`, `validate`, `test`, `compile`, and `run`. Implemented
today: `init`, `validate`, `test`, `run` (`compile` is still planned).

## `init`

Scaffold a new Weavster project into a directory.

```bash
weavster init [dir]
```

- `dir` — target directory. Defaults to the current directory (`.`).

It writes a minimal starter — `weavster.yaml`, a `flows/main.yaml`, one fixture, and a
`README.md` — that passes `weavster test` out of the box. It refuses to overwrite an
existing project (a directory that already has a `weavster.yaml`).

```text
✓ scaffolded a Weavster project in my-project
  weavster.yaml
  flows/main.yaml
  fixtures/main/basic/input.json
  fixtures/main/basic/expected.json
  README.md

next: weavster validate && weavster test
```

## `validate`

Validate a project's `weavster.yaml` against the [config schema](./config.md), and each
`flows/*.yaml` against the [flow schema](./dsl.md).

```bash
weavster validate [path]
```

- `path` — a project directory or a path to a `weavster.yaml`. Defaults to the
  current directory (`.`).

On success it prints each validated file and exits `0`:

```text
✓ weavster.yaml is valid
✓ flows/order.yaml is valid
```

On failure it prints one path-aware message per problem and exits `1`:

```text
✗ weavster.yaml
  (root): missing required property "name"
✗ flows/order.yaml
  /steps/0: property name must be valid
```

## `test`

Run a project's [fixtures](./testing.md) and compare each output against its
expected document.

```bash
weavster test [path]
```

- `path` — a project directory. Defaults to the current directory (`.`).

Each case under `fixtures/<flow>/<case>/` is parsed, run through `flows/<flow>.yaml`, and
compared to `expected.json`. See the [Testing Guide](./testing.md) for the layout. On
success:

```text
✓ order/existing-order
✓ order/new-order

2/2 fixtures passed
```

A failing case prints a diff (`-` expected, `+` actual) and the command exits `1`:

```text
✗ order/new-order
    {
  -   "priority": "normal"
  +   "priority": "high"
    }

1/2 fixtures passed
```

## `run`

Run [pipelines](./pipelines.md) — read a source, transform with a flow, write a sink.

```bash
weavster run [name]
```

- `name` — a pipeline in `pipelines/`. Omit it to run every pipeline.

Operates on the current directory. A source yields a stream of documents and each is run
through the flow and written to the sink (a `file` is one document; `stdin` is line-delimited
and streams). Progress goes to stderr so a `stdout` sink stays pipeable:

```text
✓ order (1 document)

1/1 pipelines ran
```

A startup failure (bad pipeline, source won't open) — or, on a bounded `file` source, the
document's own failure — exits `1`:

```text
✗ order
  no input file "in/order.json"
```

:::note
Install the published CLI with `npm install -g @weavster/cli`. From the tool repo during
development, run a command with `pnpm --filter @weavster/cli dev <command>`.
:::
