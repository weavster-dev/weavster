---
sidebar_position: 2
---

# Flow Configuration

Flows define how data moves through your pipeline.

## Basic Structure

```yaml title="flows/my-flow.yaml"
name: my-flow
description: Process incoming events

input:
  type: kafka
  topic: events
  group_id: weavster-processor

transforms:
  - type: map
    fields:
      event_id: "{{ id }}"
      timestamp: "{{ created_at }}"

outputs:
  - type: postgres
    table: processed_events
```

## Input Configuration

Each flow has exactly one input:

```yaml
input:
  type: <connector-type>
  # ... connector-specific options
```

See [Connectors](../concepts/connectors) for available input types.

## Transforms

Transforms are applied in order:

```yaml
transforms:
  - type: map
    fields:
      new_field: "{{ existing_field }}"

  - type: filter
    condition: "{{ status == 'active' }}"
```

See [Transforms](../concepts/transforms) for available transform types.

## Outputs

Flows can have multiple outputs:

```yaml
outputs:
  - type: postgres
    table: events

  - type: file
    path: ./backup/events.jsonl
```

## Bridges

Link flows together using bridges:

```yaml
# In flow-a.yaml
outputs:
  - type: bridge
    target: flow-b

# In flow-b.yaml
input:
  type: bridge
  source: flow-a
```
