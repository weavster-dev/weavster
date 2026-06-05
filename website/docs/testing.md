---
sidebar_position: 5
title: Testing Guide
---

# Testing Guide

`weavster test` verifies a project's [flows](./dsl.md) with fixtures: an input document, the
expected output, and a comparison between them. Each fixture's input is parsed, run through
its flow, and compared to the expected output.

## Fixture layout

Fixtures are grouped by the flow that transforms them. A fixture folder under `fixtures/`
names a flow in `flows/`, and each case inside it has one input and one expected file:

```text
my-integration/
  flows/
    order.yaml          # the flow
  fixtures/
    order/              # cases for flows/order.yaml
      new-order/
        input.json      # source document
        expected.json   # output the flow should produce
      existing-order/
        input.json
        expected.json
```

- `fixtures/<flow>/` maps to `flows/<flow>.yaml`.
- Each `<case>/` has exactly one `input.json` and one `expected.json`.
- Both files are JSON. (XML and other format packs arrive in later milestones.)

## Running

```bash
weavster test [path]
```

For each case the harness parses `input.json`, runs it through `flows/<flow>.yaml`, and
compares the result to `expected.json` with a deep equality check. It prints `✓`/`✗` per
case (labelled `<flow>/<case>`), a closing `passed` count, and exits `1` if any case fails.

A failing case shows a line-by-line diff of pretty-printed JSON — `-` is expected, `+` is
actual:

```text
✗ order/new-order
    {
      "id": "A-1001",
  -   "priority": "normal"
  +   "priority": "high"
    }
```

A flow that fails to load (missing or schema-invalid) fails every case under it; a flow that
throws while running fails that case with the transform error.

## Adding a fixture

1. Make sure the flow exists at `flows/<flow>.yaml`.
2. Create `fixtures/<flow>/<case>/` named for the case.
3. Add `input.json` with a representative source document.
4. Add `expected.json` with the output the flow should produce for that input.
5. Run `weavster test` and confirm the case passes.
