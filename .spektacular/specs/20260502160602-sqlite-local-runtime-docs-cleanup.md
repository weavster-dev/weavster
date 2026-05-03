# Feature: 20260502160602-sqlite-local-runtime-docs-cleanup

<!--
  OVERVIEW
  A concise 2-3 sentence summary of the feature. Answer three questions:
    1. What is being built?
    2. What problem does it solve?
    3. Who benefits and why does it matter?
  Avoid implementation details — this should be readable by any stakeholder.
-->
## Overview

This cleanup removes stale wording that suggests the local runtime starts or depends on embedded PostgreSQL. It keeps the current SQLite-based local runtime story consistent for users and contributors so generated starter projects and reference material do not imply unused setup requirements.

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

- [ ] **Local runtime wording matches current behavior**
  Users can read starter configuration, reference docs, and source-facing comments without seeing embedded PostgreSQL presented as the local runtime backend.

- [ ] **Starter projects avoid unused local port guidance**
  New projects should not include a local runtime port value that only made sense for the old embedded PostgreSQL framing.

- [ ] **Existing configuration compatibility is preserved**
  Existing projects that still include the local runtime port field must continue to parse successfully.


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

- Do not remove public config fields in this cleanup.
- Do not change runtime backend selection or state-store behavior.
- Keep the change small enough to replace the valid part of the closed stale PR.

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

- [ ] **No stale embedded PostgreSQL local-runtime claims remain**
  Searches across README, docs, and Rust source do not find unmarked claims that embedded PostgreSQL backs local runtime state.

- [ ] **Generated starter config omits the local port**
  `weavster init` no longer writes `runtime.local.port` into the starter `weavster.yaml`.

- [ ] **Legacy local port config still parses**
  Existing config tests continue to prove that `runtime.local.port` remains accepted for compatibility.

- [ ] **Verification passes**
  Formatting, tests, clippy, and docs checks pass for the cleanup.


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

- Update Rust comments and helper naming around local runtime config to describe SQLite-backed local state and compatibility parsing.
- Remove `runtime.local.port` from the CLI init template and docs examples while retaining the field in the config struct.
- Keep docs wording consistent with the existing current/partial/config-only status model.

<!--
  SUCCESS METRICS
  How you will know the feature is working well after delivery. Be specific:
    - Quantitative: "p99 latency < 200ms", "error rate < 0.1%"
    - Behavioural: "users complete the flow without support intervention"
  Leave blank if not applicable.
-->
## Success Metrics

- New users do not see embedded PostgreSQL or a local runtime port in generated starter config.
- Contributors scanning config code can tell that the local port field is compatibility-only and not a SQLite runtime requirement.

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

- Removing the `runtime.local.port` field from the configuration API.
- Changing SQLite or Postgres state-store selection.
- Reworking remote runtime support.
