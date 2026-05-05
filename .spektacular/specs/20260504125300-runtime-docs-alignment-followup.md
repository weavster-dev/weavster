# Feature: 20260504125300-runtime-docs-alignment-followup

<!--
  OVERVIEW
  A concise 2-3 sentence summary of the feature. Answer three questions:
    1. What is being built?
    2. What problem does it solve?
    3. Who benefits and why does it matter?
  Avoid implementation details — this should be readable by any stakeholder.
-->
## Overview

Weavster will align its command behavior and documentation around the current file-based runtime and explicit on-demand test execution. This removes misleading manual flow-run options and corrects remaining documentation drift so users and contributors understand which features work today, which are intentionally future-facing, and how the first proof of concept should behave.



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

- [ ] **Users can distinguish current runtime behavior from planned trigger behavior**
  The product presents current file-based runtime behavior accurately while keeping trigger-driven execution clearly marked as future-facing.
- [ ] **Users can run tests as explicit one-shot commands**
  Users can execute tests on demand without treating normal flow execution as a one-message or one-flow test shortcut.
- [ ] **The run command does not expose unsupported manual run modes**
  The run command no longer advertises or accepts flow selection or one-message execution options.
- [ ] **Users can trust documented runtime state configuration**
  Documentation must not claim that parsed runtime state settings are honored when the current runtime ignores them.
- [ ] **Users can understand conditional output behavior**
  Documentation must clearly state whether conditional output expressions affect runtime delivery today.
- [ ] **Users can understand transform chaining behavior**
  Documentation must distinguish sequential interpreter behavior from generated runtime behavior when they differ.
- [ ] **Users can understand lookup support limits**
  Documentation must not imply lookup transforms are usable end-to-end until lookup data is loaded into generated runtime artifacts.
- [ ] **Users can rely on documented test command configuration**
  Test execution must resolve project configuration consistently with documented command usage.
- [ ] **Developers can rely on internal docs matching implemented runtime features**
  Internal API documentation must not describe unimplemented job queue behavior as available.
- [ ] **Docs contributors can use the documented docs-site toolchain**
  Documentation-site setup instructions must match the package manager and lockfile used by the repository.



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

- Must align documentation and command behavior to the current implemented state, not planned future behavior.
- Must not implement new file-watching, event-trigger, routing, or connector runtime features in this pass.
- Must not mention the external product comparison model in the spec, README, docs, or generated documentation.
- Must not change runtime execution semantics except for removing previously accepted run options that were ignored.
- Must preserve the workspace's hexagonal architecture and keep edits scoped to CLI surface, documentation, tests, and internal docs.

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

- [ ] **Run help omits unsupported options**
  `weavster run --help` does not show flow-selection or one-message options.
- [ ] **Unsupported run options are rejected**
  Running `weavster run --flow example_flow` or `weavster run --once` fails argument parsing instead of silently accepting ignored options.
- [ ] **Run documentation matches current behavior**
  README and docs-site command references describe `weavster run` as running configured flows with current file-based behavior and do not instruct users to use flow-selection or one-message options.
- [ ] **Testing documentation owns one-shot execution**
  README and docs-site references direct users to `weavster test` for explicit on-demand verification instead of presenting normal runtime execution as a one-shot test shortcut.
- [ ] **Current runtime limits are accurately labeled**
  README and docs-site pages clearly label file connector execution as current, non-file connector execution as not current, conditional outputs as ignored or not enforced by runtime delivery, lookup transforms as not end-to-end usable, and generated transform chaining limitations where relevant.
- [ ] **Runtime state config documentation is accurate**
  README and docs-site configuration pages do not claim that parsed local data directory settings control the current CLI runtime database path unless implementation support exists.
- [ ] **Test command honors documented project configuration**
  Running tests with a non-default project configuration path loads that project rather than always looking in the current directory.
- [ ] **Internal documentation does not overstate job support**
  Generated Rust documentation no longer describes job queue management as an implemented runtime feature.
- [ ] **Docs-site setup instructions match the repository**
  The docs-site README uses the repository's current package-manager workflow and lockfile.
- [ ] **Verification passes**
  Formatting, tests, clippy, Rust documentation, and docs-site build/typecheck pass, except for any locally unavailable optional coverage tooling explicitly noted in the final report.


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

- Remove the run command's flow-selection and one-message options from the CLI parser and run command plumbing, then update integration tests and command documentation to match.
- Do not implement file-watching or event-trigger execution in this pass. Present trigger-driven execution as planned direction only, clearly separated from current behavior.
- Keep current file-based runtime processing unchanged except for no longer accepting ignored run options.
- Fix the test command's project configuration mismatch by passing the selected configuration path through the CLI to test execution.
- Align README and docs-site content around current behavior: file connector execution is current, non-file runtime I/O is not current, conditional output predicates are not enforced during delivery, lookup transforms are not end-to-end usable, generated transform behavior has chaining limits, and local runtime state currently uses the hardcoded SQLite path.
- Update internal Rust documentation so it describes implemented runtime features and does not advertise unfinished job queue management.
- Update the docs-site README to use the package-manager workflow represented by the repository lockfile.
- Preserve existing documentation structure and avoid broad redesign.


<!--
  SUCCESS METRICS
  How you will know the feature is working well after delivery. Be specific:
    - Quantitative: "p99 latency < 200ms", "error rate < 0.1%"
    - Behavioural: "users complete the flow without support intervention"
  Leave blank if not applicable.
-->
## Success Metrics

- `weavster run --help` no longer shows ignored options.
- A reviewer can read the README and docs-site command pages without finding instructions to use removed run options.
- A reviewer can classify every documented major runtime feature as current, partial, config-only, placeholder, or planned without reading source code.
- A new contributor can use the documented docs-site setup commands with the checked-in lockfile.
- CI-equivalent local verification passes after the alignment changes, except for optional coverage tooling when unavailable locally.

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

- Implementing file watching, new-file triggers, continuous event processing, or any other trigger runtime behavior.
- Implementing or changing Kafka, PostgreSQL, HTTP, Bridge, conditional routing, lookup artifact loading, package signing, or registry publishing behavior.
- Adding back manual runtime flow selection or one-message runtime execution under another option name.
- Redesigning the documentation site, changing branding, or reorganizing the documentation information architecture beyond factual alignment.
- Changing CI coverage thresholds or introducing new coverage tooling requirements.
