# Changelog - README Cleanup

## Phase 1: Update Transforms and Example Flow
**Completed**: 2026-03-19
**Status**: ✅ Complete

### What We Did
- Updated the `Example Flow` in `README.md` to use correct `map` terminology instead of `rename`.
- Swapped the non-existent `compute` transform in the example for a `template` transform.
- Updated the `Transforms` table with all currently supported transforms: `map`, `add_fields`, `template`, `filter`, `drop`, `coalesce`, `regex`, and `lookup`.

### Deviations from Plan
- None.

### Files Changed
- `README.md`

## Phase 2: Update Project Structure and Connectors
**Completed**: 2026-03-19
**Status**: ✅ Complete

### What We Did
- Added `weavster-codegen` to the `Project Structure` crate tree.
- Updated the `Connectors` list to mark `Kafka`, `Postgres`, and `HTTP` as "Partial" and placed `File` at the top as it is the most complete.

### Deviations from Plan
- Slightly reordered the connectors list for better clarity (File first).

### Files Changed
- `README.md`

## Phase 3: Add MRK "Coming Soon" Section
**Completed**: 2026-03-19
**Status**: ✅ Complete

### What We Did
- Added a new section for "MRK (Mapping, Routing & Keys)" before the "Architecture" section.
- Labeled it as "Coming Soon" with a rocket emoji and a brief description of the planned 0.2.0 features.

### Deviations from Plan
- None.

### Files Changed
- `README.md`

## Phase 4: Verification and Polish
**Completed**: 2026-03-19
**Status**: ✅ Complete

### What We Did
- Ran `make build` and `make test` to ensure no accidental breakage.
- Performed a final proofread of the documentation.

### Success Criteria Met
- [x] `make build` passed.
- [x] `make test` passed.
- [x] `grep "map:" README.md` returned matches.

## Phase 5: GitHub Workflow Integration, "Clean House" & Docker Removal
**Completed**: 2026-03-19
**Status**: ✅ Complete

### What We Did
- Created `.github/workflows/workflow-quality.yml` to automate documentation quality checks.
- Rebranded the automated check and fix to use the **`gemini-autofix`** label.
- Removed `cliff.toml` and deleted the `git-cliff` dependency from the project.
- Updated `release.yml` to automatically gather `wf-changelog.md` files from the `thoughts/` directory to generate release notes.
- **Removed all Docker references**: Deleted `docker/` directory and stripped Docker-related descriptions from `README.md`, `crates/weavster-runtime/Cargo.toml`, and `crates/weavster-runtime/src/lib.rs`.

### Deviations from Plan
- Rebranded from "Claude" to "Gemini" based on the user's current CLI tool.
- Expanded the phase to include a complete cleanup of old CI/CD changelog tools and the removal of Docker for a simpler project foundation.

### Files Changed
- `.github/workflows/workflow-quality.yml`
- `.github/workflows/release.yml`
- `README.md`
- `crates/weavster-runtime/Cargo.toml`
- `crates/weavster-runtime/src/lib.rs`
- `cliff.toml` (Deleted)
- `docker/` (Deleted)

## 🎯 FINAL SUMMARY
**Completion Date**: 2026-03-19
**Overall Status**: ✅ Complete

### What Was Built
- A comprehensive cleanup of the root `README.md` to accurately reflect the current state of the Weavster project.
- A streamlined, "Clean and Easy" documentation system centered on **Gemini-driven** workflow logs.
- Automated quality enforcement that prevents PRs from merging without documentation, with an automated `gemini-autofix` option.
- A simplified project structure free of Docker and legacy changelog dependencies.

### Key Deviations
- Consolidated the project's documentation strategy by removing `git-cliff` and relying entirely on the `wf-changelog.md` system.
- Branded all automation for Gemini CLI to match the user's interface.
- Removed Docker assets to simplify the initial development focus.

### Impact on Original Plan
The plan evolved into a significant project simplification and automation setup, providing a much cleaner and more professional foundation for future development.
