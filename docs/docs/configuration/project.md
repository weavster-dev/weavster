---
sidebar_position: 1
---

# Project Configuration

The `weavster.yaml` file is the root configuration for your Weavster project.

## Basic Structure

```yaml title="weavster.yaml"
name: my-project
version: "1.0"

# Runtime settings
runtime:
  workers: 4
  log_level: info

# Database (optional - uses embedded by default)
database:
  embedded: true
  # Or connect to external:
  # url: postgres://user:pass@host:5432/db
```

## Configuration Options

### Project Metadata

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Project name |
| `version` | string | No | Project version |

### Runtime Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `runtime.workers` | int | 4 | Number of worker threads |
| `runtime.log_level` | string | "info" | Log level (debug, info, warn, error) |

### Database Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `database.embedded` | bool | true | Use embedded PostgreSQL |
| `database.url` | string | - | External database URL |

## Environment Variables

Configuration values can reference environment variables:

```yaml
database:
  url: ${DATABASE_URL}
```

## Profiles

Use `profiles.yaml` for environment-specific overrides (similar to dbt):

```yaml title="profiles.yaml"
development:
  database:
    embedded: true

production:
  database:
    url: ${DATABASE_URL}
```

Run with a specific profile:

```bash
weavster run --profile production
```
