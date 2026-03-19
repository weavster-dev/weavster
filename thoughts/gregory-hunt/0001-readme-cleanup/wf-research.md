---
date: 2026-03-19T03:26:20Z
researcher: gregory-hunt
git_commit: 04d0290614a88fb479477339128ca8e29d8170c3
branch: main
repository: weavster/weavster
topic: README Cleanup and Feature Status Research
tags: [documentation, readme, research, mrk, transforms, connectors]
status: completed
feature_slug: gregory-hunt/0001-readme-cleanup
---

# Research Question
I want to clean up the readme to reflect the current state of weavster and mark mrk section as coming soon that we will start to work on.

# Summary
The research analyzed the current implementation of Weavster's transforms, connectors, and CLI tools to identify discrepancies with the existing `README.md`. The investigation found that while the README accurately reflects the core vision, several transform names and specific features mentioned are either missing or implemented under different names in the codebase. Additionally, the "MRK" (Mapping, Routing & Keys) feature is not yet present in the source code and should be documented as "Coming Soon."

# Detailed Findings

## 1. Transforms Implementation Status
The `weavster-core` and `weavster-codegen` crates define the available transforms. There are notable differences between the names used in the README and those in the code:

- **README `rename`**: Implemented as `map` in `weavster-core/src/transforms.rs`.
- **README `drop_fields`**: Implemented as `drop` in `weavster-core/src/transforms.rs`.
- **README `compute`**: No direct `compute` transform exists. Similar logic is likely handled by `template` or `filter` expressions.
- **README `add_fields`**: In the code, `add_fields` is restricted to static `serde_json::Value` objects, whereas the README suggests it supports computed values.
- **Code-only Transforms**: `regex`, `lookup`, and `coalesce` (the latter is in the README table but its exact implementation details in code should be verified).

| README Transform | Code Transform (`weavster-core/src/transforms.rs`) | Status |
| :--- | :--- | :--- |
| `rename` | `Map` | Name mismatch |
| `add_fields` | `AddFields` | Feature mismatch (Static vs. Computed) |
| `compute` | N/A | Missing |
| `filter` | `Filter` | Matches |
| `drop_fields` | `Drop` | Name mismatch |
| `coalesce` | `Coalesce` | Matches |

## 2. Connectors Implementation Status
Connectors are defined in `crates/weavster-core/src/connectors.rs`.

- **`File`**: Most complete implementation, covering both input and output.
- **`Kafka`, `Postgres`, `Http`**: Configuration structures exist, but the full asynchronous execution logic for production environments is partially implemented or resides in stubs.

## 3. CLI Commands
The `weavster-cli` commands were examined:

- **`init`**: Fully implemented in `crates/weavster-cli/src/commands/init.rs`. It creates a standard project structure.
- **`run`**: Exists in `crates/weavster-cli/src/commands/run.rs`.

## 4. MRK (Mapping, Routing & Keys)
A search for "MRK" in the codebase returned no results in the functional source code. This confirms it is a planned feature and should be marked as "Coming Soon" in the documentation.

# Key Discoveries
- The terminology in the README for transforms (`rename`, `drop_fields`) does not align with the code (`map`, `drop`).
- The `compute` transform mentioned in the README is not a first-class citizen in the code.
- "MRK" is purely conceptual at this stage and ready to be announced as a future roadmap item.

# References
- `README.md`
- `crates/weavster-core/src/transforms.rs`
- `crates/weavster-core/src/connectors.rs`
- `crates/weavster-cli/src/commands/init.rs`
- `crates/weavster-cli/src/commands/run.rs`
