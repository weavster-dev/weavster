---
sidebar_position: 1
---

# Transforms

Transforms manipulate data as it flows through your pipeline.

## How Transforms Work

All transforms compile to WebAssembly (WASM) for:

- **Sandboxed execution** - Transforms can't escape the WASM sandbox
- **Portable artifacts** - Same WASM runs anywhere wasmtime runs
- **Supply chain security** - OCI artifacts can be signed and verified

## Available Transforms

### Map

Rename or compute new fields:

```yaml
- type: map
  fields:
    user_id: "{{ id }}"
    full_name: "{{ first_name }} {{ last_name }}"
    created: "{{ now() }}"
```

### Filter

Include or exclude records:

```yaml
- type: filter
  condition: "{{ status == 'active' and age >= 18 }}"
```

### Regex

Pattern matching and extraction:

```yaml
- type: regex
  field: email
  pattern: "^(.+)@(.+)$"
  captures:
    username: 1
    domain: 2
```

### Lookup

Translation tables for data enrichment:

```yaml
- type: lookup
  field: country_code
  table: countries.csv
  output: country_name
```

## Jinja Templates

Transforms use MiniJinja for templating. Available functions:

| Function | Description |
|----------|-------------|
| `now()` | Current timestamp |
| `uuid()` | Generate UUID |
| `upper(s)` | Uppercase string |
| `lower(s)` | Lowercase string |
| `trim(s)` | Trim whitespace |

## Chaining Transforms

Transforms execute in order, each receiving the output of the previous:

```yaml
transforms:
  - type: map
    fields:
      email: "{{ email | lower | trim }}"

  - type: filter
    condition: "{{ email is defined }}"

  - type: regex
    field: email
    pattern: "@(.+)$"
    captures:
      domain: 1
```
