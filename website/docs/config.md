---
sidebar_position: 4
title: Config Reference
---

# Config Reference

A Weavster project is configured by a single `weavster.yaml` file at its root. This
page documents schema version `v0alpha2`. The schema lives in the tool repo at
`spec/schemas/project.schema.json` and is what `weavster validate` checks against.

## Top-level fields

| Field         | Required | Type   | Description                                                      |
| ------------- | -------- | ------ | ---------------------------------------------------------------- |
| `apiVersion`  | yes      | string | Must equal `weavster/v0alpha2`.                                  |
| `name`        | yes      | string | Lowercase letters, digits, and hyphens (`^[a-z0-9][a-z0-9-]*$`). |
| `description` | no       | string | Human-readable description of the project.                       |

Unknown top-level keys are rejected, so a typo surfaces as a validation error
rather than being silently ignored.

## Example

```yaml
apiVersion: weavster/v0alpha2
name: orders-to-warehouse
description: Map incoming order JSON to the warehouse intake format.
```

## Validating

Run [`weavster validate`](./cli.md) to check a project against this schema.

:::note
`weavster validate` also checks every `flows/*.yaml` against the
[flow schema](./dsl.md). The project's `apiVersion` governs the DSL version used by its flows.
:::
