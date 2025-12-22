---
sidebar_position: 2
---

# Your First Flow

This guide walks you through creating your first Weavster data flow.

## Initialize a Project

```bash
weavster init my-project
cd my-project
```

This creates the following structure:

```
my-project/
├── weavster.yaml          # Project configuration
├── flows/
│   └── example.yaml       # Example flow definition
└── connectors/
    └── .gitkeep
```

## Understanding the Flow

A flow defines how data moves from an input, through transforms, to outputs:

```yaml title="flows/example.yaml"
name: example
description: Example flow

input:
  type: file
  path: ./data/input.json

transforms:
  - type: map
    fields:
      user_id: "{{ id }}"
      full_name: "{{ first_name }} {{ last_name }}"

output:
  type: file
  path: ./data/output.json
```

## Run the Flow

```bash
weavster run
```

Weavster will:
1. Start an embedded PostgreSQL instance (for job queue)
2. Load your flow configuration
3. Process data from input through transforms to output

## Next Steps

- [Configuration Reference](../configuration/project) - Learn all configuration options
- [Transforms](../concepts/transforms) - Available transform types
- [Connectors](../concepts/connectors) - Input/output connectors
