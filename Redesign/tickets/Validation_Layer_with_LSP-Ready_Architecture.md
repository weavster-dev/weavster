# Validation Layer with LSP-Ready Architecture

## Overview

Implement comprehensive validation for Weavster configurations with clear, actionable error messages. Design with future LSP integration in mind (for VS Code extension), but implement as CLI-first for MVP.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/e13dad9c-eda8-46d1-be4f-09ce88c29a23` - Component Architecture (Validation Layer)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 2 (Develop a New Flow)

## Scope

**In Scope:**
- Create validation module in `file:crates/weavster-core/src/` (or new crate `weavster-validation`)
- Schema validation (required fields, valid transform types, correct YAML structure)
- Reference validation (connectors exist, flows exist, macros are defined)
- Logical validation (no circular dependencies, no unreachable code)
- Use existing YAML/Jinja validators as packages (don't reinvent)
- Focus validation logic on Weavster-specific semantics
- Error formatting with file path, line number, error message, suggested fix
- Design validation as a library that can be wrapped by LSP later

**Out of Scope:**
- LSP server implementation (post-MVP)
- VS Code extension (post-MVP)
- Runtime validation (handled by transform engine)
- WASM compilation validation (handled by codegen)

## Key Architectural Decisions

1. **Validation as Library:** Core validation logic in a reusable library
   - CLI uses library directly for MVP
   - Future LSP server wraps same library
   - Avoids duplication and ensures consistency

2. **Leverage Existing Validators:**
   - Use `serde_yaml` for YAML syntax validation
   - Use `minijinja` for Jinja syntax validation
   - Use `regex` crate for regex pattern validation
   - Focus custom code on Weavster-specific semantics

3. **Error Message Format:**
   ```
   Error: Invalid transform type 'mapp' in flow 'customer_enrichment'
     --> flows/customer_enrichment.yaml:12:5
      |
   12 |   - mapp:
      |     ^^^^ unknown transform type
      |
      = help: Did you mean 'map'? Available transforms: map, drop, add_fields, filter
   ```

4. **Validation Levels:**
   - **Syntax:** YAML is well-formed, Jinja is valid
   - **Schema:** Required fields present, types correct
   - **References:** Connectors/flows/macros exist
   - **Logic:** No circular deps, no unreachable code

## Validation Checks

### Schema Validation
- Flow has required fields: `name`, `input`, `transforms`, `output`
- Transforms have correct structure for their type
- Connector configs have required fields
- Test definitions have required fields

### Reference Validation
- Input/output connectors exist in `connectors/`
- Macro references point to existing macros
- Bridge connectors reference valid flows
- Test definitions reference existing flows

### Logical Validation
- No circular macro references
- No circular bridge connector loops
- Filter conditions use valid operators
- Field references are consistent

### Jinja Validation
- Jinja syntax is valid
- Jinja functions are supported (`now()`, `env`, `macro()`)
- Jinja variables are defined in context

## Acceptance Criteria

- [ ] Schema validation catches missing required fields
- [ ] Schema validation catches invalid transform types
- [ ] Reference validation catches missing connectors
- [ ] Reference validation catches missing macros
- [ ] Logical validation catches circular macro references
- [ ] Jinja syntax validation works
- [ ] Error messages include file path, line number, and suggestion
- [ ] Validation library is reusable (not CLI-specific)
- [ ] Unit tests cover all validation scenarios
- [ ] Integration tests validate complete projects

## Testing Strategy

**Unit Tests:**
- Each validation rule has positive and negative test cases
- Error message formatting is tested
- Edge cases: empty files, malformed YAML, missing references

**Integration Tests:**
- Validate complete valid project (should pass)
- Validate project with various errors (should fail with correct messages)
- Validate after macro expansion
- Validate with different profiles

## Dependencies

- **Depends on:** Config Layer (Ticket 1) - needs parsed config to validate

## Estimated Effort

2 days (Phase 1 of implementation plan)
