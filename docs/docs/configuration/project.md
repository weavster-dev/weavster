---
sidebar_position: 1
---

# Project Configuration

`weavster.yaml` is the root project configuration file.

## Basic Structure

```yaml title="weavster.yaml"
name: my-project
version: "0.1.0"

runtime:
  mode: local
  local:
    data_dir: ".weavster/data"

vars:
  environment: development

profiles:
  production:
    runtime:
      mode: remote
      remote:
        postgres_url: "{{ env('WEAVSTER_PG_URL') }}"
        redis_url: "{{ env('REDIS_URL') }}"
    vars:
      environment: production

error_handling:
  on_error: log_and_skip
  log_level: warn

macros_dir: macros
```

## Project Fields

| Field | Status | Description |
| --- | --- | --- |
| `name` | Current | Project name |
| `version` | Current | Project version, defaults to `0.1.0` |
| `runtime.mode` | Config-only | Parsed from config, but it does not currently select the runtime backend |
| `runtime.local.data_dir` | Config-only | Parsed from config; current CLI runtime state still uses the default SQLite path |
| `runtime.local.port` | Compatibility-only | Legacy field accepted by config parsing; not used by SQLite local state and not included in new starter projects |
| `runtime.remote.postgres_url` | Config-only | Parsed from config; current CLI runtime selects Postgres state only from the `WEAVSTER_PG_URL` environment variable |
| `runtime.remote.redis_url` | Planned | Modeled in config; distributed Redis runtime is not implemented |
| `vars` | Current | Static variables available for config-level Jinja substitution |
| `profiles` | Current | Inline environment-specific overrides |
| `error_handling` | Partial | Parsed and used by current transform/runtime paths where errors are surfaced |
| `macros_dir` | Current | Directory containing macro definitions |

## Runtime State

Local runtime state currently uses SQLite at `.weavster/data/local.db`. The `runtime.local.data_dir` field is parsed for compatibility, but the current CLI runtime does not use it to choose the SQLite database path.

Postgres state is available when `WEAVSTER_PG_URL` is set in the environment. Setting only `runtime.mode: remote` or `runtime.remote.postgres_url` in `weavster.yaml` does not currently switch the CLI runtime away from the local SQLite path. The `remote` config shape exists, but remote/distributed runtime behavior is not complete.

## Environment Variables

Config-level Jinja expressions can read environment variables:

```yaml
profiles:
  production:
    runtime:
      mode: remote
      remote:
        postgres_url: "{{ env('WEAVSTER_PG_URL') }}"
```

Unknown runtime template expressions are preserved for later transform/runtime handling.

## Profiles

Profiles are currently loaded from the `profiles` map inside `weavster.yaml`:

```yaml
profiles:
  development:
    vars:
      log_level: debug

  production:
    vars:
      log_level: warn
```

Run with a profile:

```bash
weavster run --profile production
```

`weavster init` also writes a separate `profiles.yaml` file. That file is planned for alignment; current config loading expects profiles inline in `weavster.yaml`.
