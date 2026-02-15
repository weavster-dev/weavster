# Foundation: Config Layer with Jinja & Macro Support

## Overview

Implement the configuration layer that loads, parses, and validates Weavster project configurations. This is the foundation for all other components and must support YAML parsing, Jinja templating (static and dynamic), macro expansion, and profile resolution.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/e13dad9c-eda8-46d1-be4f-09ce88c29a23` - Component Architecture (Config Layer)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 4 (Macros), Flow 6 (Profiles)

## Scope

**In Scope:**
- Enhance `file:crates/weavster-core/src/config.rs` to support:
  - YAML parsing with comprehensive error messages
  - Jinja static evaluation (load-time) for config values
  - Jinja dynamic evaluation (runtime) for transform values like `{{ now() }}`
  - Macro expansion from `macros/` directory
  - Profile resolution (dev/prod) with hierarchical overrides
  - Error handling configuration (global → flow → transform hierarchy)
- Update `file:crates/weavster-core/src/flow.rs` to support new config structure
- Add support for bridge connector configuration
- Implement config caching to avoid re-parsing

**Out of Scope:**
- WASM compilation (next ticket)
- Deep validation logic (separate ticket; basic structural validation of expanded output is in scope)
- CLI commands (separate ticket)
- Database integration (later ticket)

## Key Architectural Decisions

1. **Jinja Evaluation Strategy:** Hybrid approach
   - Static values evaluated at config load time (e.g., `environment: "{{ env }}"`)
   - Dynamic values evaluated at runtime per-message (e.g., `processed_at: "{{ now() }}"`)
   - Use `minijinja` crate (already in workspace deps)

2. **Macro Expansion:** Load-time expansion
   - Macros defined in `macros/*.yaml`
   - Referenced via `{{ macro('name') }}`
   - Expanded inline during config parsing
   - Downstream validation (separate ticket) operates on expanded transforms

3. **Profile Resolution:**
   - Profiles defined in `weavster.yaml`
   - Selected via `--profile` flag or `WEAVSTER_PROFILE` env var
   - Hierarchical override: global defaults → profile → flow → transform

4. **Error Handling Config:**
   - Hierarchical: global → flow → transform
   - Fields: `on_error` (log_and_skip | stop_on_error), `log_level`, `retry` config

## Implementation Details

### Config Structure

```yaml
# weavster.yaml
profiles:
  dev:
    runtime:
      mode: local
      database: sqlite
    connectors:
      kafka: file
  prod:
    runtime:
      mode: remote
      database: postgres

error_handling:
  on_error: log_and_skip
  log_level: info
  retry:
    max_attempts: 3
    backoff: exponential
```

### Macro Example

```yaml
# macros/normalize_phone.yaml
name: normalize_phone
description: Normalize phone numbers
transforms:
  - regex:
      field: phone
      pattern: '^\+?1?(\d{3})(\d{3})(\d{4})$'
      output: '+1$1$2$3'
```

### Flow with Macro Reference

```yaml
# flows/customer.yaml
name: customer_enrichment
transforms:
  - {{ macro('normalize_phone') }}
  - add_fields:
      processed_at: "{{ now() }}"
      environment: "{{ env }}"
```

## Acceptance Criteria

- [ ] Config layer can parse `weavster.yaml` with profiles
- [ ] Jinja static evaluation works for config values
- [ ] Jinja dynamic evaluation context is prepared (executed later by transform engine)
- [ ] Macros are loaded from `macros/` directory and expanded inline
- [ ] Profile resolution works with `--profile` flag
- [ ] Error handling config is parsed with hierarchical overrides
- [ ] Bridge connector config is supported
- [ ] Comprehensive error messages for YAML syntax errors
- [ ] Unit tests cover all config parsing scenarios
- [ ] Config caching prevents redundant parsing

## Testing Strategy

**Unit Tests:**
- YAML parsing with valid/invalid syntax
- Jinja static evaluation with various expressions
- Macro expansion with nested macros
- Profile resolution with different profiles
- Error handling hierarchy resolution
- Edge cases: missing files, circular macro references, invalid Jinja

**Integration Tests:**
- Load complete project config with all features
- Profile switching changes config correctly
- Macro expansion produces correct transform chain

## Dependencies

None - this is the foundation ticket.

## Estimated Effort

2-3 days (Phase 1 of implementation plan)
