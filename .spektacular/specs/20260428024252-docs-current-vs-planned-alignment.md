# Feature: 20260428024252-docs-current-vs-planned-alignment

<!--
  OVERVIEW
  A concise 2-3 sentence summary of the feature. Answer three questions:
    1. What is being built?
    2. What problem does it solve?
    3. Who benefits and why does it matter?
  Avoid implementation details — this should be readable by any stakeholder.
-->
## Overview

Weavster documentation will clearly distinguish what users can rely on today from what is planned or only partially built. This prevents users and contributors from following stale or aspirational guidance and gives maintainers a shared baseline for documentation, feature status, and test health.

<!--
  REQUIREMENTS
  Specific, testable behaviours the feature must deliver.
  Format: bold title on the checkbox line, detail indented below.
  Rules:
    - Use active voice: "Users can...", "The system must..."
    - Each requirement should be independently verifiable
    - Focus on WHAT, not HOW — avoid prescribing implementation
    - Keep each item atomic — one behaviour per line
-->
## Requirements

- [ ] **Users can identify currently usable capabilities**
  Users can tell which features are available for normal use today without inferring from marketing copy or roadmap language.
- [ ] **Users can distinguish planned and partial capabilities**
  Users can see when a capability is planned, partially built, configuration-only, or otherwise not ready for end-to-end use.
- [ ] **Users can follow a working getting-started path**
  New users can run the documented setup and example flow using commands and configuration that match the current product.
- [ ] **Contributors can understand documentation health**
  Contributors can see the current documentation gaps and where the user-facing docs need alignment with the product.
- [ ] **Contributors can understand test health**
  Contributors can see what verification currently passes, what coverage is configured, and what test gaps remain.
- [ ] **Documentation does not overstate runtime support**
  The documentation must avoid presenting parsing, scaffolding, or placeholder behavior as fully working runtime functionality.

<!--
  CONSTRAINTS
  Hard boundaries the solution must operate within. These are non-negotiable.
  Examples:
    - Must integrate with the existing authentication system
    - Cannot introduce breaking changes to the public API
    - Must support the current minimum supported runtime versions
  Leave blank if there are no constraints.
-->
## Constraints

- Must not change runtime, CLI, library, packaging, or test behavior.
- Must not document unsupported commands, install paths, configuration syntax, or runtime integrations as working.
- Must preserve the existing license positioning and contribution workflow unless the documentation is correcting stale factual claims.
- Must keep future roadmap items separate from current capabilities.

<!--
  ACCEPTANCE CRITERIA
  The specific, binary conditions that define "done".
  Format: bold title on the checkbox line, verifiable detail indented below.
  Each criterion must be:
    - Independently verifiable (pass/fail, not subjective)
    - Traceable back to a requirement above
    - Testable by someone who didn't write the code
-->
## Acceptance Criteria

- [ ] **Current capabilities are labeled**
  The top-level project documentation contains a clearly labeled current-state summary covering CLI commands, runtime connectors, transform support, configuration support, packaging, and state storage.
- [ ] **Planned and partial capabilities are labeled**
  The top-level project documentation contains a separate planned or partial feature summary that includes Kafka, PostgreSQL, HTTP, Bridge routing, MRK, remote runtime behavior, OCI signing or registry publishing, and placeholder management commands.
- [ ] **The quick start works against the current product**
  The documented quick-start path uses the current install/build path, connector reference syntax, and generated example project shape.
- [ ] **Runtime limitations are visible**
  A reader can see that end-to-end runtime execution currently supports file-based connectors only, and that other connector types are not presented as production-ready runtime integrations.
- [ ] **Transform limitations are visible**
  A reader can see which transforms are available in the current generated WASM flow path and which transform behaviors are still incomplete or limited.
- [ ] **Documentation drift is corrected or explicitly marked**
  Getting-started, configuration, connector, transform, and CLI docs no longer present unsupported syntax, missing commands, or placeholder behavior as fully implemented.
- [ ] **Test status is documented**
  The documentation states the local verification commands that pass, the known local coverage-tool gap, and the mismatch between configured coverage policy and the repository requirement.
- [ ] **No behavior changes are required**
  The change can be reviewed as documentation-only, with no changes to runtime, CLI, or library behavior.

<!--
  TECHNICAL APPROACH
  High-level technical direction to guide the planning agent. Include:
    - Key architectural decisions already made
    - Preferred patterns or technologies if known
    - Integration points with existing systems
    - Known risks or areas of uncertainty
  Leave blank if you want the planner to propose the approach.
-->
## Technical Approach

- Reorganize `README.md` around a current-state summary, a feature status matrix, a current quick start, known limitations, planned features, and test/documentation health.
- Align stale Docusaurus pages under `docs/docs/` with the same status model, especially getting started, configuration, connectors, transforms, and CLI command reference.
- Base all status labels on the audited source code and local verification results from this repository.
- Keep the work documentation-only. Do not add implementation behavior to make the docs true.
- Use concise status labels such as current, partial, config-only, placeholder, and planned so readers can scan the state quickly.

<!--
  SUCCESS METRICS
  How you will know the feature is working well after delivery. Be specific:
    - Quantitative: "p99 latency < 200ms", "error rate < 0.1%"
    - Behavioural: "users complete the flow without support intervention"
  Leave blank if not applicable.
-->
## Success Metrics

- A reviewer can classify every documented major capability as current, partial, config-only, placeholder, or planned without reading source code.
- A new contributor can run the documented source-based quick start and see the generated sample flow process JSONL data.
- A documentation review finds no remaining claims that embedded PostgreSQL, push/pull commands, inline flow connector syntax, or non-file runtime connectors are fully supported today.
- Local verification commands and coverage-policy status are visible from the documentation without inspecting CI configuration.

<!--
  NON-GOALS
  Explicitly state what this spec does NOT cover. This is as important as
  the requirements — it prevents scope creep and sets clear expectations.
  Examples:
    - "Mobile support is out of scope (tracked in #456)"
    - "Internationalisation will be addressed in a follow-up spec"
  Leave blank if there are no explicit exclusions to call out.
-->
## Non-Goals

- Implementing Kafka, PostgreSQL, HTTP, Bridge, filter, conditional routing, push/pull, status, flow management, connector management, or package signing behavior.
- Changing the CLI interface, runtime architecture, test suite, CI workflows, or coverage thresholds.
- Rebranding the project or redesigning the documentation site.
- Publishing hosted install scripts, release binaries, or external website changes.
