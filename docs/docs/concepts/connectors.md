---
sidebar_position: 2
---

# Connectors

Connectors are adapters for reading from and writing to external systems.

## Input Connectors

### Kafka

```yaml
input:
  type: kafka
  brokers:
    - localhost:9092
  topic: events
  group_id: weavster-consumer
  auto_offset_reset: earliest
```

### File

```yaml
input:
  type: file
  path: ./data/input.jsonl
  format: jsonl  # json, jsonl, csv
  watch: true    # Watch for new files
```

### HTTP

```yaml
input:
  type: http
  port: 8080
  path: /webhook
```

### Postgres

```yaml
input:
  type: postgres
  url: postgres://user:pass@host:5432/db
  query: "SELECT * FROM events WHERE processed = false"
  poll_interval: 5s
```

## Output Connectors

### Kafka

```yaml
output:
  type: kafka
  brokers:
    - localhost:9092
  topic: processed-events
```

### File

```yaml
output:
  type: file
  path: ./data/output.jsonl
  format: jsonl
```

### Postgres

```yaml
output:
  type: postgres
  url: postgres://user:pass@host:5432/db
  table: processed_events
  on_conflict: upsert
  conflict_columns:
    - id
```

### HTTP

```yaml
output:
  type: http
  url: https://api.example.com/events
  method: POST
  headers:
    Authorization: "Bearer ${API_TOKEN}"
```

## Bridge Connector

Connect flows together:

```yaml
# Flow A output
output:
  type: bridge
  target: flow-b

# Flow B input
input:
  type: bridge
  source: flow-a
```
