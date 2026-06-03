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

:::note
The `weavster` binary is not yet published. From the tool repo, run the command
during development with `pnpm --filter @weavster/cli dev validate <path>`.
:::
