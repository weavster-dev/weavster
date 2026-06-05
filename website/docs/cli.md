---
sidebar_position: 3
title: CLI Reference
---

# CLI Reference

The `weavster` CLI runs against a project directory containing a `weavster.yaml`.

The planned commands are `init`, `validate`, `test`, `compile`, and `run`. Only the
commands documented below are implemented today (`init`, `validate`, `test`).

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

:::note
The `weavster` binary is not yet published. From the tool repo, run a command
during development with `pnpm --filter @weavster/cli dev <command> <path>`.
:::
