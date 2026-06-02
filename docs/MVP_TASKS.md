# Weavster MVP Task List

## How to use this list

This task list is designed to keep the reboot visible, sequential, and reviewable. It should be used alongside `MVP_PLAN.md`, with one small task or tightly related pair of tasks per branch or stacked PR whenever possible.[cite:126][cite:42][cite:97][cite:101]

The operating rule is simple: do not start a new unchecked section until the current section has passing tests, updated docs, and a commit or PR that can be explained clearly from memory. This supports your goal of using a coding agent while still understanding the codebase as it evolves.[cite:3][cite:126]

## Working rules

- [ ] Work from top to bottom; do not skip ahead unless blocked.
- [ ] Keep one active milestone at a time.
- [ ] Create a short branch or stacked PR for each small slice.[cite:97][cite:99]
- [ ] Require tests before marking implementation tasks done.[cite:101][cite:104]
- [ ] Require docs updates before marking a feature complete.[cite:147][cite:150]
- [ ] Write a short note after each merged slice explaining what changed and what you learned.
- [ ] If Claude Code or another agent writes code, review every changed file before merge.[cite:3]
- [ ] If a task feels too large for one clear review, split it before starting.

## Code understanding checklist

Use this on every meaningful task before merge.

- [ ] Can the change be explained in two or three sentences without reading the diff?
- [ ] Is the behavior covered by a test or fixture?
- [ ] Is the reason for the change captured in docs, spec text, or commit message?
- [ ] Are refactors separated from behavior changes?
- [ ] Can the CLI or example prove the change works locally?
- [ ] Is there any new abstraction that exists for only one use case? If yes, simplify it.

## Milestone 0 — Reboot foundation

### Repo reset

- [x] Freeze current repo state with a legacy branch or archive tag. _(N/A — greenfield repo, only CLAUDE.md + LICENSE tracked)_
- [x] Create reboot branch. _(N/A — repo is the reboot)_
- [x] Remove or archive code that does not support the new MVP thesis. _(N/A — no legacy code)_
- [x] Create the new top-level folder structure from `MVP_PLAN.md`.
- [x] Add or update `.gitignore`, formatter, and editor config.

### Planning artifacts

- [x] Commit `MVP_PLAN.md`.
- [x] Commit this `MVP_TASKS.md` file.
- [x] Add `CONTRIBUTING.md` with small-PR and testing rules.
- [x] Add PR template with docs and test checklist.
- [x] Add issue labels or project board columns matching milestones. _(labels M0–M9 on GitHub)_

### Personal control

- [x] Create a `notes/DEV_LOG.md` or equivalent work journal.
- [x] Add a template entry for “what changed / what I learned / what is next.”
- [x] Decide the maximum PR size rule for yourself, for example one concept per PR. _(one concept per PR — see CONTRIBUTING.md)_

## Milestone 1 — Documentation platform and guardrails

### Docs scaffold

- [x] Create Docusaurus site in `website/`.[cite:145]
- [x] Configure title, base URL, repo links, and navigation.[cite:145][cite:150]
- [x] Create initial sidebar structure.
- [x] Create placeholder pages: Getting Started, Concepts, CLI, Config, Testing, Architecture, Contributing.

### Docs operations

- [x] Add docs build command to local workflow. _(root `docs:build` / `docs:start` scripts)_
- [x] Add GitHub Actions job to build docs on PRs.[cite:150][cite:152]
- [x] Add GitHub Actions job to deploy docs on merge to default branch.[cite:150][cite:152]
- [x] Document the docs update policy in `CONTRIBUTING.md`. _(added in M0)_
- [x] Add docs review checklist to PR template. _(added in M0)_

### Understanding checkpoint

- [x] Build docs locally from scratch.
- [x] Verify you understand where nav, pages, and config live.
- [x] Write a short dev log entry on how docs are built and deployed.

## Milestone 2 — Config schema and validation

### Config spec

- [x] Create `spec/schemas/project.schema.json`.
- [x] Define `v0alpha1` top-level config structure.[cite:49][cite:112][cite:121]
- [x] Decide required top-level keys. _(`apiVersion`, `name`)_
- [x] Decide which unknown properties are rejected. _(all — `additionalProperties: false`)_
- [x] Create one valid sample config.
- [x] Create at least three invalid sample configs. _(4 under `spec/examples/project/`)_

### Validation command

- [x] Add YAML loading.
- [x] Add schema validation with Ajv.[cite:49][cite:125]
- [x] Implement `weavster validate`.
- [x] Print useful path-aware validation errors.[cite:117][cite:125]
- [x] Add tests for valid and invalid configs.

### Docs and understanding

- [x] Write config reference page.
- [x] Write `validate` command docs.
- [x] Confirm you can explain the difference between schema validation and deeper compile-time validation. _(see DEV_LOG M2 entry)_
- [x] Add a dev log entry with one config that failed and why.

## Milestone 3 — Fixture test harness

### Fixture design

- [ ] Define fixture folder layout.
- [ ] Decide fixture naming convention.
- [ ] Define input, expected output, and assertions structure.
- [ ] Add first failing fixture-based test.

### Test command

- [ ] Implement fixture loader.
- [ ] Implement actual vs expected comparison.
- [ ] Implement readable diff output.
- [ ] Add `weavster test` command.
- [ ] Add tests covering passing and failing fixtures.

### Docs and understanding

- [ ] Write testing guide page.
- [ ] Document how to add a new fixture.
- [ ] Run the test harness manually on at least two examples.
- [ ] Add a dev log entry on how fixture tests flow through the code.

## Milestone 4 — Canonical document model

### Model design

- [ ] Define internal normalized node structure.
- [ ] Define path addressing rules.
- [ ] Define metadata fields for source format and validation messages.
- [ ] Decide what information is preserved from raw input.

### Implementation

- [ ] Add canonical model types.
- [ ] Add path access helpers.
- [ ] Add tests for nested objects, arrays, and path lookups.
- [ ] Add tests showing the same transform target can work across formats later.

### Docs and understanding

- [ ] Write canonical model concept page.
- [ ] Add one diagram or table explaining the model.
- [ ] Trace one input document manually through normalization.
- [ ] Add a dev log entry explaining why this model exists.

## Milestone 5 — JSON format pack

### JSON support

- [ ] Create JSON format pack structure.
- [ ] Implement JSON parser.
- [ ] Implement JSON serializer.
- [ ] Map JSON into canonical model.
- [ ] Add round-trip tests.

### Docs and understanding

- [ ] Write JSON format pack docs.
- [ ] Add one JSON example to the golden path.
- [ ] Confirm you understand every step from raw JSON to normalized model to output.

## Milestone 6 — XML format pack

### XML support

- [ ] Create XML format pack structure.
- [ ] Implement XML parser.
- [ ] Implement XML serializer.
- [ ] Map XML into canonical model.
- [ ] Add validation abstraction for future XSD support.[cite:71][cite:74]
- [ ] Add round-trip tests.

### Docs and understanding

- [ ] Write XML format pack docs.
- [ ] Document current XML limitations.
- [ ] Compare one JSON example and one XML example side by side in the docs.
- [ ] Add a dev log entry on where XML handling differs from JSON.

## Milestone 7 — Declarative transform DSL

### DSL design

- [ ] Define the first supported transform operations.
- [ ] Decide YAML shape for each operation.
- [ ] Decide how errors are reported for bad mappings.
- [ ] Add failing tests for each operation before implementing.

### Implementation

- [ ] Implement `map`.
- [ ] Implement `rename` or equivalent field remap primitive.
- [ ] Implement `default`.
- [ ] Implement `concat`.
- [ ] Implement conditional logic.
- [ ] Implement minimal string/date helpers.
- [ ] Add tests for each operation and at least one combined pipeline.

### Docs and understanding

- [ ] Write DSL reference docs.
- [ ] Add copy-paste examples.
- [ ] Add one “when not to use config” note.
- [ ] Walk through one transform execution path in the debugger or logs.
- [ ] Add a dev log entry summarizing the execution path.

## Milestone 8 — TypeScript escape hatch

### Contract design

- [ ] Define custom TypeScript step contract.
- [ ] Define input and output boundaries.
- [ ] Decide how TypeScript code is loaded and executed.[cite:53][cite:56]
- [ ] Decide how safety and validation errors are surfaced.

### Implementation

- [ ] Add runtime support for custom TypeScript step.
- [ ] Add contract validation around custom step input/output.
- [ ] Add tests for successful custom steps.
- [ ] Add tests for failing custom steps.

### Docs and understanding

- [ ] Write TypeScript transforms docs.
- [ ] Add one minimal custom transform example.
- [ ] Add one rule-of-thumb section: config first, TypeScript second.
- [ ] Add a dev log entry on where custom code enters and leaves the system.

## Milestone 9 — Golden-path example and developer experience

### Example project

- [ ] Generate `examples/golden-path/` via `weavster init` (not hand-built).
- [ ] Confirm the generated layout matches the user project layout in `MVP_PLAN.md`.
- [ ] Fill in sample config (`weavster.yaml`, `flows/`).
- [ ] Add sample fixtures (`fixtures/<case>/input.*`).
- [ ] Add sample expected outputs (`fixtures/<case>/expected.*`).
- [ ] Ensure `validate`, `test`, and `run` work against the example.
- [ ] Treat this example as the contract for `weavster init` output — if it drifts, fix `init` or the layout spec.

### Developer docs

- [ ] Update README quickstart.
- [ ] Add “first 30 minutes with Weavster” guide.
- [ ] Add architecture overview page.
- [ ] Link docs sections in the intended reading order.

### CI and release readiness

- [ ] Run golden-path example in CI.
- [ ] Run docs build in CI.
- [ ] Confirm repo can be cloned and used from a clean machine.
- [ ] Write a release checklist for the first MVP tag.

## After-MVP backlog parking lot

These are intentionally not active now. Keep them visible, but do not pull them into current work unless MVP is complete.

- [ ] HL7 v2 format pack.[cite:70][cite:73]
- [ ] X12 format pack with careful standards/licensing separation.[cite:64][cite:65]
- [ ] Additional transports such as SFTP, file, REST, and TCP.
- [ ] Stronger XML contract support with XSD-backed validation.[cite:74]
- [ ] WASM power-user plugin path.
- [ ] MCP-friendly docs retrieval index.[cite:126][cite:4]

## Per-task execution template

Use this template whenever starting a new task:

### Task

- Name:
- Why now:
- Expected files:
- Test to add first:
- Docs to update:
- Smallest acceptable outcome:

### After finishing

- [ ] Tests pass locally.
- [ ] Docs updated.
- [ ] Diff reviewed file by file.
- [ ] Commit message is clear.
- [ ] Dev log updated.
- [ ] Next task selected.

## Weekly review checklist

Run this once a week so the project stays understandable.

- [ ] Which milestone is active?
- [ ] What changed this week?
- [ ] Which tasks were finished?
- [ ] Which tasks expanded unexpectedly?
- [ ] Which abstractions were introduced, and were they justified?
- [ ] Are docs still aligned with actual behavior?
- [ ] Can the golden path still be explained simply?
- [ ] What is the single most important next slice?

