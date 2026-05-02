# Feature: 20260502165842-workflow-quality-spektacular-alignment

<!--
  OVERVIEW
  A concise 2-3 sentence summary of the feature. Answer three questions:
    1. What is being built?
    2. What problem does it solve?
    3. Who benefits and why does it matter?
  Avoid implementation details — this should be readable by any stakeholder.
-->
## Overview

Pull request quality checks should match the current Spektacular documentation workflow instead of enforcing the retired Gemini changelog process. This prevents valid code PRs from failing on obsolete automation while keeping a lightweight release-note check for contributors and reviewers.

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

- [ ] **Code PRs are checked against current release-note artifacts**
  Pull requests that change Rust code should satisfy the quality check when they update the root changelog or add Spektacular spec/plan artifacts for the change.

- [ ] **Retired Gemini automation is no longer part of PR quality gating**
  Contributors should not be told to use Gemini labels, Gemini autofix, or the old workflow changelog skill to make PR checks pass.

- [ ] **Maintainers can request Codex changelog generation**
  Same-repository pull requests with missing release-note evidence can be labeled for Codex to add a root changelog entry automatically.

- [ ] **Existing quality job names remain stable where practical**
  The workflow should keep recognizable check names so existing PR review habits and branch protection expectations are not disrupted unnecessarily.


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

- Do not change release or nightly publishing workflows in this PR.
- Do not add a replacement auto-generation bot in this PR.
- Do not weaken Rust CI, docs CI, coverage, or formatting checks.

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

- [ ] **Code changes with current changelog artifacts pass the quality gate**
  A PR that changes Rust code and updates the root changelog or includes Spektacular artifacts is not failed for missing `wf-changelog.md`.

- [ ] **Old Gemini instructions are removed from pull request quality output**
  The workflow no longer references `gemini-autofix`, `wf-update-changelog`, `wf-changelog.md`, or Gemini CLI in PR quality jobs.

- [ ] **Codex autofix updates only the root changelog**
  When a same-repository code PR is labeled for autofix, the workflow asks Codex to update `CHANGELOG.md` and does not create retired workflow changelog artifacts.

- [ ] **Quality workflow remains valid GitHub Actions YAML**
  The workflow can be parsed by GitHub Actions and keeps the PR quality check active for pull requests to main.

- [ ] **Verification passes**
  YAML checks and repository grep checks confirm the PR quality workflow is aligned with Spektacular.


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

- Update the pull request quality workflow to detect Rust code changes and then look for current documentation artifacts: root `CHANGELOG.md` changes, `.spektacular/specs/**`, or `.spektacular/plans/**`.
- Keep the existing `Check Workflow Changelog` and `Fail if Missing` job names, but update their behavior and messages to current terminology.
- Replace the Gemini autofix path with `openai/codex-action@v1` for same-repository PRs labeled `codex-autofix`.
- Keep Codex generation scoped to root `CHANGELOG.md`; Spektacular artifacts are produced by the normal Spektacular workflow, not CI.

<!--
  SUCCESS METRICS
  How you will know the feature is working well after delivery. Be specific:
    - Quantitative: "p99 latency < 200ms", "error rate < 0.1%"
    - Behavioural: "users complete the flow without support intervention"
  Leave blank if not applicable.
-->
## Success Metrics

- Open code PRs such as the codegen optimization and SQLite cleanup are not blocked by the obsolete `wf-changelog.md` requirement after they include current changelog/Spektacular artifacts or rebase onto the fixed workflow.
- Same-repository code PRs with `codex-autofix` receive a generated root changelog entry instead of a failure.

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

- Updating release or nightly release workflows that still reference Gemini.
- Requiring every documentation-only PR to include Spektacular artifacts.
- Running Codex autofix on fork PRs.
- Generating Spektacular specs or plans from CI.
