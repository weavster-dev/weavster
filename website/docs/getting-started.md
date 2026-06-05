---
sidebar_position: 1
title: Getting Started
---

# Getting Started

Your first 30 minutes with Weavster: scaffold a project, run it, and change a transform.

## Install

```bash
npm install -g @weavster/cli
```

Now `weavster` is on your PATH. Working from a clone of the
[tool repo](https://github.com/weavster-dev/weavster) instead? Run `pnpm install` then
`pnpm cli:link` to build and link the CLI locally.

## 1. Scaffold a project

```bash
weavster init my-integration
cd my-integration
```

This writes a minimal, working project:

```text
my-integration/
  weavster.yaml          # project config (apiVersion + name)
  flows/main.yaml        # a transform pipeline
  fixtures/main/basic/   # an example input + expected output
  README.md
```

## 2. Validate it

```bash
weavster validate
```

`validate` checks `weavster.yaml` and every `flows/*.yaml` against their schemas:

```text
✓ weavster.yaml is valid
✓ flows/main.yaml is valid
```

## 3. Test it

```bash
weavster test
```

`test` runs each fixture's `input.json` through its flow and compares the result to
`expected.json`:

```text
✓ main/basic

1/1 fixtures passed
```

## 4. Change a transform

Open `flows/main.yaml`. The starter sets a field:

```yaml
steps:
  - _set:
      status: new
```

Add a step that builds a value from the input (see the [Transform DSL](./dsl.md)):

```yaml
steps:
  - _set:
      status: new
      label: { _concat: { parts: [order, $id], sep: '-' } }
```

Run `weavster test` again. It now fails, showing a diff — the output gained a `label` the
fixture doesn't expect:

```text
✗ main/basic
    {
      "id": "demo-1",
      "status": "new",
  +   "label": "order-demo-1"
    }
```

Update `fixtures/main/basic/expected.json` to include `"label": "order-demo-1"`, then
`weavster test` passes again. That loop — change a flow, run the fixtures — is the core of
working in Weavster.

## Where to go next

- [Concepts](./concepts.md) — the canonical model and paths every transform operates on.
- [Transform DSL](./dsl.md) — the full operator set.
- [Format Packs](./formats.md) — JSON and XML in and out.
- [TypeScript Transforms](./typescript.md) — the escape hatch for logic the DSL can't express.
- [Testing Guide](./testing.md) — how fixtures work in depth.
