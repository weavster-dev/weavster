# CLI Polish, Documentation & End-to-End Testing

## Overview

Polish the CLI with discovery commands, enhanced UX, and complete the documentation. This is the final ticket that brings all components together and ensures the MVP is ready for use.

**Spec References:**
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/71f5f5ce-0ce5-44c1-826e-997166b1accd` - Flow 7 (Discover and Learn)
- `spec:3afb66cd-a148-48b8-8ce9-0bf5860c562d/ae5101d9-bf0f-4073-b8a0-1576e4feea30` - Epic Brief (LLM-friendly design)

## Scope

**In Scope:**
- CLI Enhancements:
  - `weavster list transforms` - show available transforms with descriptions
  - `weavster list connectors` - show available connector types
  - `weavster list flows` - show flows in current project
  - Better error messages with suggestions
  - Progress bars for long-running operations
  - Color-coded output throughout
- Documentation:
  - Complete Docusaurus site in `file:docs/`
  - Tutorial examples (customer enrichment, data filtering, etc.)
  - `llm.txt` file for LLM consumption
  - README updates with quick start guide
  - API reference for all transforms
  - Best practices guide
- End-to-End Testing:
  - Complete workflow tests (init → validate → test → run)
  - Performance benchmarks
  - Error scenario tests
  - Cross-platform testing (Linux, macOS, Windows)
- Final Polish:
  - Consistent error messages
  - Helpful suggestions in errors
  - Clean up debug logging
  - Optimize hot paths

**Out of Scope:**
- VS Code extension (post-MVP)
- Advanced tutorials (post-MVP)
- Video tutorials (post-MVP)
- Community examples repository (post-MVP)

## Key Architectural Decisions

1. **Discovery Commands:** Comprehensive listing
   - `list transforms`: Show all transforms with examples
   - `list connectors`: Show connector types and config options
   - `list flows`: Show flows in current project with status

2. **Error Message Quality:** Actionable and helpful
   - Include context (file, line, what went wrong)
   - Suggest fixes when possible
   - Link to documentation for complex errors
   - Use colors for readability

3. **Documentation Structure:**
   ```
   docs/
   ├── docs/
   │   ├── index.md (Getting Started)
   │   ├── getting-started/
   │   │   ├── installation.md
   │   │   └── first-flow.md
   │   ├── concepts/
   │   │   ├── transforms.md
   │   │   └── connectors.md
   │   ├── configuration/
   │   │   ├── project.md
   │   │   └── flows.md
   │   ├── cli/
   │   │   └── commands.md
   │   └── tutorials/
   │       ├── customer-enrichment.md
   │       └── data-filtering.md
   ├── llm.txt (LLM-friendly summary)
   └── README.md
   ```

4. **LLM.txt Format:** Structured for LLM consumption
   - Project overview
   - Quick start guide
   - All transform types with examples
   - Common patterns
   - Error troubleshooting

## Implementation Details

### List Commands

```rust
pub fn list_transforms_command() -> Result<()> {
    println!("Available Transforms:\n");

    println!("  map");
    println!("    Rename or remap fields");
    println!("    Example:");
    println!("      - map:");
    println!("          customer_id: id");
    println!("          customer_name: name\n");

    println!("  drop");
    println!("    Remove fields from records");
    println!("    Example:");
    println!("      - drop:");
    println!("          - internal_id");
    println!("          - temp_field\n");

    println!("  add_fields");
    println!("    Add new fields with static or dynamic values");
    println!("    Example:");
    println!("      - add_fields:");
    println!("          processed_at: \"{{ now() }}\"");
    println!("          version: \"1.0\"\n");

    println!("  filter");
    println!("    Filter records based on conditions");
    println!("    Example:");
    println!("      - filter:");
    println!("          and:");
    println!("            - field: status");
    println!("              equals: \"active\"");
    println!("            - field: age");
    println!("              gte: 18\n");

    Ok(())
}

pub fn list_connectors_command() -> Result<()> {
    println!("Available Connectors:\n");

    println!("  file (Input & Output)");
    println!("    Read/write files with glob pattern support");
    println!("    Config:");
    println!("      type: file");
    println!("      path: ./data/input/*.jsonl");
    println!("      format: jsonl");
    println!("      on_processed: move  # move | delete | leave\n");

    println!("  bridge (Input & Output)");
    println!("    Connect flows via in-memory queue");
    println!("    Config:");
    println!("      type: bridge");
    println!("      name: customer_bridge\n");

    Ok(())
}

pub fn list_flows_command() -> Result<()> {
    let config = load_config(".")?;

    println!("Flows in current project:\n");

    for flow in config.flows {
        println!("  {} ({})", flow.name.green(), flow.input.type_);
        println!("    {} transforms", flow.transforms.len());
        println!("    Output: {}", flow.output.type_);
        println!();
    }

    Ok(())
}
```

### Progress Bars

```rust
use indicatif::{ProgressBar, ProgressStyle};

pub fn process_with_progress(total: usize) -> Result<()> {
    let pb = ProgressBar::new(total as u64);
    pb.set_style(
        ProgressStyle::default_bar()
            .template("[{elapsed_precise}] {bar:40.cyan/blue} {pos}/{len} {msg}")
            .progress_chars("##-")
    );

    for i in 0..total {
        // Process record
        pb.set_message(format!("Processing record {}", i));
        pb.inc(1);
    }

    pb.finish_with_message("Done");
    Ok(())
}
```

### llm.txt Structure

```markdown
# Weavster - Modern Data Transformation Engine

## Overview
Weavster is a WASM-based data transformation engine for enterprise integration.
Think dbt, but for real-time file processing and data interoperability.

## Quick Start
1. Install: Download binary from releases
2. Init: `weavster init my-project`
3. Validate: `weavster validate`
4. Test: `weavster test`
5. Run: `weavster run --profile dev`

## Transforms
### map
Rename or remap fields
Example: ...

### drop
Remove fields
Example: ...

### add_fields
Add new fields
Example: ...

### filter
Filter records
Example: ...

## Common Patterns
### Customer Enrichment
...

### Data Filtering
...

## Troubleshooting
### Compilation Errors
...

### File Watching Issues
...
```

## Acceptance Criteria

- [ ] `weavster list transforms` shows all transforms with examples
- [ ] `weavster list connectors` shows connector types
- [ ] `weavster list flows` shows flows in current project
- [ ] Error messages include context and suggestions
- [ ] Progress bars show for long operations
- [ ] Colors are used consistently throughout CLI
- [ ] Docusaurus site is complete with all sections
- [ ] Tutorial examples are working and tested
- [ ] `llm.txt` is comprehensive and LLM-friendly
- [ ] README has quick start guide
- [ ] API reference documents all transforms
- [ ] End-to-end tests cover complete workflows
- [ ] Performance benchmarks establish baseline
- [ ] Cross-platform tests pass (Linux, macOS, Windows)

## Testing Strategy

**End-to-End Tests:**
- Complete workflow: init → validate → test → run
- Error scenarios: invalid config, missing files, compilation failures
- Performance: process 10K records, measure throughput
- Cross-platform: run on Linux, macOS, Windows

**Documentation Tests:**
- All code examples in docs are valid
- Tutorial examples run successfully
- Links in docs are not broken

**UX Tests:**
- Error messages are helpful
- Progress bars work correctly
- Colors render correctly in different terminals

## Dependencies

- **Depends on:** All previous tickets (this is the final integration ticket)

## Estimated Effort

3-4 days (Phase 6 of implementation plan)

## Success Criteria

MVP is complete when:
- ✅ `weavster init` creates working project
- ✅ `weavster validate` catches configuration errors
- ✅ `weavster compile` compiles flows to WASM
- ✅ `weavster test` runs user tests successfully
- ✅ `weavster run` processes files continuously
- ✅ All 4 transforms (map, drop, add_fields, filter) work
- ✅ SQLite state management works
- ✅ Documentation is complete
- ✅ All tests pass (unit + integration + e2e)
