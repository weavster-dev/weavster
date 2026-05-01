---
sidebar_position: 1
---

# Transforms

Transforms manipulate JSON records as they move through a flow. Support varies by layer: YAML parsing, generated WASM code, direct interpreter support, and end-to-end runtime behavior are not always the same.

## Status Summary

| Transform | Status | Notes |
| --- | --- | --- |
| `map` | Current | Used by the generated starter flow and supported by the direct interpreter |
| `drop` | Current | Used by the generated starter flow and supported by the direct interpreter |
| `add_fields` | Current | Used by the generated starter flow and supported by the direct interpreter |
| `coalesce` | Partial | Parsed, interpreted, and code-generated; not in the starter flow |
| `regex` | Partial | Parsed and code-generated; not supported by the direct interpreter |
| `template` | Partial | Parsed and code-generated; not supported by the direct interpreter |
| `lookup` | Partial | Parsed and code-generated; lookup artifact loading is limited |
| `filter` | Partial | Parsed, but generated runtime behavior is currently pass-through/incomplete |

## Current Starter Transforms

### Map

Copy a field into a new field name.

```yaml
- map:
    full_name: name
    email: email
```

### Drop

Remove fields from the current record.

```yaml
- drop:
    - name
    - age
```

### Add Fields

Add static JSON values.

```yaml
- add_fields:
    processed: true
```

Dynamic helpers such as `{{ now() }}`, `{{ uuid() }}`, and `{{ timestamp() }}` are handled only in supported dynamic-evaluation paths. Treat them as partial rather than generally available in every transform.

## Other Parsed Transform Shapes

These transform shapes are accepted by the config/parser layers and have generated-code support in some cases, but they should be treated as partial until the runtime behavior is fully validated.

### Coalesce

```yaml
- coalesce:
    email:
      - primary_email
      - secondary_email
      - backup_email
```

### Regex

```yaml
- regex:
    field: email
    pattern: "^(.+)@(.+)$"
    captures:
      username: "1"
      domain: "2"
```

### Template

```yaml
- template:
    full_name: "{{ first_name }} {{ last_name }}"
```

### Lookup

```yaml
- lookup:
    field: country_code
    table: country_names
    output: country_name
    default: "Unknown"
```

### Filter

```yaml
- filter:
    when: "status == 'active'"
```

Filter expression support is incomplete in the generated runtime path. Do not rely on filters or conditional output expressions for production routing yet.

## Chaining Transforms

Transforms execute in order. Keep starter flows to current transforms unless you are intentionally testing partial behavior.

```yaml
transforms:
  - map:
      full_name: name

  - drop:
      - name

  - add_fields:
      processed: true
```
