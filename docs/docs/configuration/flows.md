---
sidebar_position: 2
---

# Flow Configuration

Flows define how data moves from one input connector reference, through transforms, to one or more output connector references.

## Basic Structure

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

## Connector References

Flow `input` and `outputs` use connector references, not inline connector definitions.

```yaml
input: file.input
outputs:
  - file.output
```

The reference format is `filename.key`. `file.input` maps to the `input` entry in `connectors/file.yaml`.

## Transforms

Transforms are applied in order. The current starter path uses `map`, `drop`, and `add_fields`.

```yaml
transforms:
  - map:
      full_name: name

  - drop:
      - name

  - add_fields:
      processed: true
```

Other transform types are parsed or generated in some layers, but support varies. See [Transforms](../concepts/transforms).

## Outputs

Simple outputs are connector references:

```yaml
outputs:
  - file.output
```

Conditional output syntax is parsed:

```yaml
outputs:
  - connector: file.high_value
    when: "total > 1000"
```

Conditional output expression enforcement is partial today and should not be treated as a complete routing feature.

## Flow-Level Options

| Field | Status | Description |
| --- | --- | --- |
| `name` | Current | Flow name |
| `description` | Current | Optional description |
| `input` | Current | Connector reference |
| `transforms` | Current | Ordered transform list, with support varying by transform |
| `outputs` | Current | Output connector references |
| `vars` | Current | Flow-level variables for static config substitution |
| `error_handling` | Partial | Flow-level error behavior for supported execution paths |

## Bridge Flows

Bridge connector configuration is parsed, but bridge runtime processing is not implemented end-to-end. Treat Bridge as config-only until runtime support lands.
