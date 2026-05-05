---
sidebar_position: 2
---

# Your First Flow

This guide uses the current supported path: a generated project with file connectors, JSONL input, generated WASM transforms, and SQLite local state.

## Initialize a Project

```bash
weavster init my-project
cd my-project
```

This creates:

```text
my-project/
├── weavster.yaml
├── profiles.yaml
├── flows/
│   └── example_flow.yaml
├── connectors/
│   └── file.yaml
├── tests/
└── data/
    └── input.jsonl
```

## Flow Definition

The generated flow uses connector references. `file.input` resolves to the `input` entry in `connectors/file.yaml`; `file.output` resolves to the `output` entry.

```yaml title="flows/example_flow.yaml"
name: example_flow
description: An example flow to get you started

input: file.input

transforms:
  - map:
      full_name: name
      email: email

  - drop:
      - name
      - age

  - add_fields:
      processed: true

outputs:
  - file.output
```

## Connector Definition

```yaml title="connectors/file.yaml"
input:
  type: file
  path: "./data/input.jsonl"
  format: jsonl

output:
  type: file
  path: "./data/output.jsonl"
  format: jsonl
```

## Run the Flow

```bash
weavster run
```

Weavster will:

1. Load the project and flow configuration.
2. Compile the flow to a cached WASM artifact.
3. Read records from `data/input.jsonl`.
4. Apply the transform pipeline.
5. Write records to `data/output.jsonl`.
6. Store local runtime state in SQLite under `.weavster/data/local.db`.

## Current Limits

- The end-to-end runtime path currently supports file connectors only.
- Local state uses SQLite, not embedded PostgreSQL.
- `weavster run` processes available file input records and exits. Use `weavster test` for explicit one-shot flow checks.

## Next Steps

- [Project Configuration](../configuration/project) - Learn current project config fields
- [Flow Configuration](../configuration/flows) - Learn flow YAML shape
- [Transforms](../concepts/transforms) - See transform support by layer
- [Connectors](../concepts/connectors) - See connector support by status
