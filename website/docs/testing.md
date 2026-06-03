---
sidebar_position: 5
title: Testing Guide
---

# Testing Guide

`weavster test` verifies a project's transforms with fixtures: an input document,
the expected output, and a comparison between them.

## Fixture layout

Fixtures live in the project's `fixtures/` directory, one folder per case:

```text
my-integration/
  weavster.yaml
  fixtures/
    order-passthrough/
      input.json       # source document
      expected.json    # output the flow should produce
```

- One folder per case; the folder name is the case name shown in test output.
- Each case has exactly one `input.json` and one `expected.json`.
- Both files are JSON. (XML and other format packs arrive in later milestones.)

## Running

```bash
weavster test [path]
```

For each case the harness reads `input.json`, runs the project's flow, and compares
the result to `expected.json` with a deep equality check. It prints `✓`/`✗` per case,
a closing `passed` count, and exits `1` if any case fails.

A failing case shows a line-by-line diff of pretty-printed JSON — `-` is expected,
`+` is actual:

```text
✗ changed
    {
  -   "a": 2
  +   "a": 1
    }
```

## Adding a fixture

1. Create a folder under `fixtures/` named for the case.
2. Add `input.json` with a representative source document.
3. Add `expected.json` with the output the flow should produce for that input.
4. Run `weavster test` and confirm the case passes.

:::note
M3 runs an identity passthrough: there is no transform engine yet, so output equals
input and a case passes when `expected.json` matches `input.json`. As transforms land
in later milestones, `expected.json` captures the transformed result; the harness and
fixture layout stay the same.
:::
