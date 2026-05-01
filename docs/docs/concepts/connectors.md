---
sidebar_position: 2
---

# Connectors

Connectors describe external systems that flows read from and write to. Current support differs between configuration parsing and runtime execution.

## Status Summary

| Connector | Status | Runtime notes |
| --- | --- | --- |
| File | Current | Supported for end-to-end runtime input and output; JSONL is the exercised path |
| Kafka | Config-only | Config can be parsed, but runtime connector I/O is not implemented |
| PostgreSQL | Config-only | Config can be parsed, but runtime connector I/O is not implemented |
| HTTP | Config-only | Config can be parsed, but runtime connector I/O is not implemented |
| Bridge | Config-only | Config can be parsed, but bridge processing is not implemented |

## Connector Files

Flow files refer to connectors by `filename.key`.

```yaml title="flows/example_flow.yaml"
input: file.input
outputs:
  - file.output
```

That maps to entries in `connectors/file.yaml`:

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

## File Connector

File connector runtime support is current for JSONL input/output.

```yaml
input:
  type: file
  path: "./data/input.jsonl"
  format: jsonl

output:
  type: file
  path: "./data/output.jsonl"
  format: jsonl
```

The config model has a `format` field, but JSONL is the current exercised path. File watching and continuous polling are planned rather than current behavior.

## Kafka Connector

Kafka connector config is parsed, but runtime Kafka consumption/production is not implemented.

```yaml
orders:
  type: kafka
  brokers:
    - localhost:9092
  topic: orders
  group_id: weavster-consumer
```

Use this as a config-shape reference only until runtime support lands.

## PostgreSQL Connector

PostgreSQL connector config is parsed, but runtime table read/write connector I/O is not implemented.

```yaml
orders:
  type: postgres
  url: postgres://user:pass@localhost/db
  table: orders
  schema: public
```

This is separate from the runtime state store. Local state currently uses SQLite; Postgres state is selected by `WEAVSTER_PG_URL`.

## HTTP Connector

HTTP connector config is parsed, but runtime webhook/API connector I/O is not implemented.

```yaml
webhook:
  type: http
  url: https://api.example.com/events
  method: POST
  headers:
    Authorization: "Bearer token"
```

## Bridge Connector

Bridge connector config is parsed, but bridge queue processing between flows is not implemented end-to-end.

```yaml
internal:
  type: bridge
  queue_table: flow_bridge
  batch_size: 100
  poll_interval_ms: 500
  lease_duration_ms: 30000
```
