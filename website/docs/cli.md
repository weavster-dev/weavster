---
sidebar_position: 3
title: CLI Reference
---

# CLI Reference

The `weavster` CLI runs against a project directory containing a `weavster.yaml`.

The planned commands are `init`, `validate`, `test`, `compile`, and `run`. Only the
commands documented below are implemented today.

## `validate`

Validate a project's `weavster.yaml` against the [config schema](./config.md).

```bash
weavster validate [path]
```

- `path` — a project directory or a path to a `weavster.yaml`. Defaults to the
  current directory (`.`).

On success it prints the validated file and exits `0`:

```text
✓ weavster.yaml is valid
```

On failure it prints one path-aware message per problem and exits `1`:

```text
✗ weavster.yaml
  (root): missing required property "name"
  /apiVersion: must equal "weavster/v0alpha1"
```

## `test`

Run a project's [fixtures](./testing.md) and compare each output against its
expected document.

```bash
weavster test [path]
```

- `path` — a project directory. Defaults to the current directory (`.`).

Each fixture case under `fixtures/<case-name>/` is read, run through the project's
flow, and compared to `expected.json`. On success:

```text
✓ order-passthrough

1/1 fixtures passed
```

A failing case prints a diff (`-` expected, `+` actual) and the command exits `1`:

```text
✗ changed
    {
  -   "a": 2
  +   "a": 1
    }

0/1 fixtures passed
```

:::note
M3 runs an identity passthrough: with no transform engine yet, output equals input,
so a fixture passes when `expected.json` matches `input.json`. The transform DSL
(later milestones) changes what the flow produces, not how `weavster test` works.
:::

:::note
The `weavster` binary is not yet published. From the tool repo, run a command
during development with `pnpm --filter @weavster/cli dev <command> <path>`.
:::
