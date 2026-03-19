---
date: 2026-03-19T03:28:51Z
author: gregory-hunt
git_commit: 04d0290614a88fb479477339128ca8e29d8170c3
branch: main
repository: weavster/weavster
topic: README Cleanup and Feature Status Research
tags: [documentation, readme, implementation, mrk, transforms, connectors]
status: planned
feature_slug: gregory-hunt/0001-readme-cleanup
---

# Overview
This plan outlines the necessary updates to the `README.md` to align the documentation with the actual implementation state of the Weavster project. This includes correcting transform names, updating the project structure, and adding a "Coming Soon" section for the MRK feature.

# Current State Analysis
- The `README.md` uses outdated transform names (`rename`, `drop_fields`) and mentions a `compute` transform that isn't implemented.
- The project structure section misses the `weavster-codegen` crate.
- The status of several connectors is not clearly documented as "Partially Implemented" or "Configuration Only."
- The "MRK" (Mapping, Routing & Keys) concept is not mentioned.

# Desired End State
- `README.md` terminology matches the `weavster-core` codebase (`map`, `drop`).
- The `Example Flow` accurately reflects current syntax.
- The `Transforms` table is complete and reflects the actual feature set.
- `weavster-codegen` is included in the project structure.
- An "MRK" section is added as "Coming Soon."

# What We're NOT Doing
- Implementing new transforms or connectors.
- Changing any functional code.
- Updating Docusaurus documentation (this plan is for the root `README.md`).

# Implementation Approach
The updates will be made in phases, focusing on different sections of the `README.md` to ensure clarity and consistency.

# Project References
- `Makefile` (for verification commands)

# Phases

## Milestone 1: Alignment and Documentation
**Goal**: Ensure the primary documentation is accurate and reflects the current codebase.

### Phase 1: Update Transforms and Example Flow
**Overview**: Correct the transform names in the example and the transforms table.
**Changes Required**:
- [x] `README.md`: Update `Example Flow` block to use `map` instead of `rename`.
- [x] `README.md`: Update `Transforms` table with `map`, `drop`, `regex`, `lookup`, `template`, `filter`, `coalesce`, and `add_fields`.
- [x] `README.md`: Remove `compute` transform or mark it as planned.
**Success Criteria**:
- **Automated Verification**: `grep "map:" README.md` should find matches.
- **Manual Verification**: Review the `README.md` for consistency in transform naming.

### Phase 2: Update Project Structure and Connectors
**Overview**: Include missing crates and clarify connector status.
**Changes Required**:
- [x] `README.md`: Add `weavster-codegen` to the `Project Structure` list.
- [x] `README.md`: Mark `Kafka`, `PostgreSQL`, and `HTTP` connectors as "Partially Implemented" or "Configuration-only" where appropriate.
**Success Criteria**:
- **Manual Verification**: Ensure all crates in `crates/` are represented in the README.

### Phase 3: Add MRK "Coming Soon" Section
**Overview**: Document the upcoming MRK feature.
**Changes Required**:
- [x] `README.md`: Add a new section for "MRK (Mapping, Routing & Keys)" with a "Coming Soon" badge and a brief description.
**Success Criteria**:
- **Manual Verification**: Confirm the MRK section is clear and clearly marked as future work.

### Phase 5: GitHub Workflow Integration
**Overview**: Automate the verification and auto-generation of workflow documentation.
**Changes Required**:
- [x] `.github/workflows/workflow-quality.yml`: Create a new workflow that checks for `wf-changelog.md` in PRs and offers an `claude-autofix` label for auto-generation.
**Success Criteria**:
- **Manual Verification**: Verify the YAML structure and triggers.

## Milestone 2: Final Review and Validation
**Goal**: Verify all documentation changes are correct and maintain a professional appearance.

### Phase 4: Verification and Polish
**Overview**: Run existing build and test commands to ensure no accidental breakage and perform a final proofread.
**Changes Required**: None (verification only).
**Success Criteria**:
- [x] **Automated Verification**:
  - `make build` passes.
  - `make test` passes.
- [x] **Manual Verification**: Final proofread of `README.md`.

# References
- `thoughts/gregory-hunt/0001-readme-cleanup/wf-research.md`
- `crates/weavster-core/src/transforms.rs`
- `crates/weavster-core/src/connectors.rs`
