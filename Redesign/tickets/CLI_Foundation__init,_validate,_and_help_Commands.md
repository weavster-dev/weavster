# CLI Foundation: init, validate, and help Commands

## Overview

Implement the foundational CLI commands that enable the first-time user experience: `init`, `validate`, and comprehensive `--help`. This establishes the CLI structure and user interaction patterns for all future commands.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 1 (First-Time User Onboarding)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/ae5101d9-bf0f-4073-b8a0-1576e4feea30` - Epic Brief (LLM-friendly design)

## Scope

**In Scope:**
- Enhance `file:crates/weavster-cli/src/main.rs` and `file:crates/weavster-cli/src/commands/`
- Implement `weavster init <project-name>` command
  - Create project directory structure
  - Generate example flow with single transform
  - Create sample input data (JSONL)
  - Create example test case
  - Display next steps to user
- Implement `weavster validate` command
  - Use validation library from Ticket 2
  - Display validation results with colors (green ✓, red ✗)
  - Show errors with file path, line number, suggestions
- Implement comprehensive `--help` for all commands
  - Top-level help guides user to `init`
  - Each command has detailed help with examples
  - LLM-friendly: clear, consistent, comprehensive
- Add `.gitignore` generation in init command

**Out of Scope:**
- `run` command (later ticket)
- `test` command (later ticket)
- `compile` command (later ticket)
- `list` commands (polish ticket)

## Key Architectural Decisions

1. **CLI Framework:** Use `clap` (already in workspace)
   - Derive-based API for clean command definitions
   - Automatic help generation
   - Subcommand structure

2. **Project Template:**
   ```
   my-project/
   ├── .gitignore
   ├── weavster.yaml
   ├── flows/
   │   └── example_flow.yaml
   ├── connectors/
   │   └── file.yaml
   ├── macros/
   ├── tests/
   │   ├── example_test.yaml
   │   └── fixtures/
   │       ├── input.jsonl
   │       └── expected_output.jsonl
   ├── data/
   │   └── input.jsonl
   └── .weavster/
       ├── cache/
       └── logs/
   ```

3. **Example Flow:** Simple, working example
   - Single input file connector
   - Two transforms: `map` (rename field) and `add_fields` (add timestamp)
   - Single output file connector
   - Demonstrates basic functionality

4. **Output Formatting:**
   - Use `colored` crate for terminal colors
   - Green ✓ for success
   - Red ✗ for errors
   - Yellow ! for warnings
   - Clear, actionable messages

## Implementation Details

### init Command

```rust
// Pseudocode structure
fn init_command(project_name: &str) -> Result<()> {
    // Create directory structure
    create_dir_all(format!("{}/flows", project_name))?;
    create_dir_all(format!("{}/connectors", project_name))?;
    create_dir_all(format!("{}/macros", project_name))?;
    create_dir_all(format!("{}/tests/fixtures", project_name))?;
    create_dir_all(format!("{}/data", project_name))?;
    create_dir_all(format!("{}/.weavster/cache", project_name))?;

    // Generate files from templates
    write_file("weavster.yaml", WEAVSTER_YAML_TEMPLATE)?;
    write_file("flows/example_flow.yaml", EXAMPLE_FLOW_TEMPLATE)?;
    write_file("connectors/file.yaml", FILE_CONNECTOR_TEMPLATE)?;
    write_file("tests/example_test.yaml", EXAMPLE_TEST_TEMPLATE)?;
    write_file("data/input.jsonl", SAMPLE_DATA)?;
    write_file(".gitignore", GITIGNORE_TEMPLATE)?;

    // Display next steps
    println!("✓ Created Weavster project: {}", project_name);
    println!("\nNext steps:");
    println!("  cd {}", project_name);
    println!("  weavster validate");
    println!("  weavster test");
    println!("  weavster run --profile dev");
}
```

### validate Command

```rust
fn validate_command() -> Result<()> {
    // Load config
    let config = load_config(".")?;

    // Run validation
    let results = validate(&config)?;

    // Display results
    if results.is_valid() {
        println!("✓ Validation passed");
        println!("  {} flows validated", results.flow_count);
        println!("  {} connectors validated", results.connector_count);
    } else {
        for error in results.errors {
            eprintln!("✗ {}", error.format_with_context());
        }
        exit(1);
    }
}
```

## Acceptance Criteria

- [ ] `weavster init my-project` creates complete project structure
- [ ] Generated project includes working example flow
- [ ] Generated project includes sample data and test
- [ ] `.gitignore` is created with appropriate entries (`.weavster/cache/`, `.weavster/logs/`)
- [ ] `weavster validate` runs validation and displays results
- [ ] Validation errors show file path, line number, and suggestions
- [ ] `weavster --help` displays comprehensive help
- [ ] Each command has detailed help with examples
- [ ] Output uses colors for better readability
- [ ] CLI integration tests verify commands work end-to-end

## Testing Strategy

**Unit Tests:**
- Template generation produces valid YAML
- Directory creation handles existing directories
- Error formatting is correct

**Integration Tests:**
- `weavster init test-project` creates valid project
- `cd test-project && weavster validate` passes
- Invalid configs produce correct error messages
- Help text is comprehensive and accurate

## Dependencies

- **Depends on:** Config Layer (Ticket 1) - for loading config
- **Depends on:** Validation Layer (Ticket 2) - for validate command

## Estimated Effort

1-2 days (Phase 1 of implementation plan)
