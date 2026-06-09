# Weavster MVP Plan

## Product thesis

Weavster MVP is a developer-first, config-driven integration tool that lets developers define transformation pipelines in YAML, validate them locally, test them with fixtures, and run them through a modular execution engine.[cite:84][cite:49][cite:54]

The first MVP is intentionally narrow. Its job is not to become a full modern ESB in one release; its job is to prove that config-first authoring, local validation, current documentation, and testable transformations can replace the fragile point-and-click integration workflow common in older enterprise tools.[cite:84][cite:101][cite:104][cite:147]

## MVP goals

The MVP should prove six things:

- A developer can create a project from a simple config structure and understand it quickly.[cite:84]
- Config can be validated locally with clear errors before runtime.[cite:49][cite:54]
- Transform behavior can be tested with fixtures and expected outputs on a local machine.[cite:101][cite:104]
- Multiple formats can plug into one common pipeline model rather than requiring special-case engines.[cite:70][cite:73][cite:74]
- Advanced users can extend the system with TypeScript without forcing every user to write code.[cite:53][cite:56]
- Documentation can be published automatically and kept aligned with the codebase through docs-as-code workflow and CI guardrails.[cite:145][cite:147][cite:150]

## Non-goals

These items are explicitly out of scope for MVP:

- No enterprise control plane.
- No visual designer.
- No distributed cluster orchestration.
- No broad connector catalog beyond what is needed for the golden path.
- No custom WASM authoring experience for end users.
- No full healthcare or logistics standards coverage in v1.
- No complex scheduling, HA, multi-tenant governance, or hosted SaaS features.

The MVP should optimize for clarity, testability, local developer experience, and continuously accurate docs rather than completeness.[cite:101][cite:104][cite:147]

## Tool repo vs user project

There are two distinct repositories, and this document describes the first one.

- **This repo is the Weavster tool.** It holds the engine source: CLI, core, format packs, built-in functions, and the TypeScript runtime. It also ships the schema, docs site, and one reference example. End users do not edit this repo.
- **A Weavster project is a separate repo owned by the user.** It holds only config, fixtures, and expected outputs. The user creates it by running `weavster init` in their own directory, then runs `weavster validate`, `weavster test`, `weavster compile`, and `weavster run` against it.

The CLI is the boundary between the two. The tool repo defines behavior and schema; the user project supplies data and config. The tool never assumes its own layout when running against a user project — it works from the project directory it is pointed at.

### User project layout

`weavster init` scaffolds this shape in the user's directory:

```text
my-integration/
  weavster.yaml          # project config, schema version v0alpha1
  flows/                 # transform pipelines, one concept per file
  fixtures/
    <case-name>/
      input.<ext>        # source document (json, xml, ...)
      expected.<ext>     # expected output for `weavster test`
  README.md
```

This layout is the contract `weavster init` must produce and the layout `weavster validate|test|run` must accept. It is defined once here so the example below and the CLI scaffolder do not drift apart.

### Where fixtures live

The word "fixtures" appears in two roles; keep them separate:

- **Tool-test fixtures** (`tests/fixtures/`, `spec/examples/`) verify the tool itself — schema validation cases, format round-trips, DSL operations. They are CI inputs for the engine.
- **User-project fixtures** (inside a project's `fixtures/`) verify a user's transforms. `weavster test` runs these.

`examples/golden-path/` is a reference **user project** living inside the tool repo. It must match the user project layout above — it is exactly what `weavster init` produces — and CI exercises it as a smoke test of the user-facing path.

## Golden path

The MVP should center on one complete golden-path workflow:

1. A developer creates a Weavster project.
2. The project contains YAML config, fixtures, and expected outputs.
3. The CLI validates the config against schema rules.
4. The runtime parses input into a canonical internal model.
5. Declarative transform steps run against that model.
6. The result is emitted in a target format.
7. Local tests confirm the output matches the expected fixture.
8. The docs site explains this exact flow and is updated in the same change set as product behavior.

This golden path is the product. Every MVP milestone should strengthen it, not distract from it.[cite:3][cite:101][cite:147]

## MVP scope

### Included

- CLI with `init`, `validate`, `test`, `compile`, and `run`.
- YAML config model with versioning.
- JSON Schema validation using Ajv-compatible schemas.[cite:49][cite:54][cite:60]
- Canonical document model.
- Format packs for JSON and XML.[cite:71][cite:74]
- Declarative transform DSL with a small built-in function set.
- Fixture-based test harness.
- Minimal TypeScript escape hatch for advanced transforms.[cite:53][cite:56]
- Docusaurus documentation site with docs stored in-repo and deployed automatically.[cite:130][cite:145][cite:150]
- One complete example project.

### Excluded

- HL7 v2 support in the first pass.
- X12 support in the first pass.
- UI/dashboard for the product runtime.
- Hosted runtime.
- Multi-node scaling work.
- Premium/enterprise features.
- Arbitrary plugin marketplace.

HL7 and X12 are important future format packs, but the MVP should first establish the common modular architecture that will later support them.[cite:64][cite:70][cite:73]

## Architecture constraints

The reboot should follow these constraints:

- The tool is config-first, not code-first.
- TypeScript is the first advanced escape hatch.
- WASM is an internal implementation detail or later power-user extension, not the center of authoring in MVP.[cite:53][cite:56]
- The engine should be modular around transport, format, contract, transform, and runtime stages.[cite:70][cite:73][cite:74]
- The docs system should be docs-as-code, versioned, and deployed from the repository source of truth.[cite:130][cite:145][cite:147]
- Every feature must improve local validate, test, compile, run, or documentation workflows.
- No new abstraction is added until at least two real use cases require it.

## Repository shape

A clean rebooted repository should look like this:

```text
weavster/
  docs/
    MVP_PLAN.md
    architecture/
    product/
  website/
    docusaurus.config.ts
    sidebars.ts
    src/
  spec/
    schemas/
    examples/
  cli/
  core/
  formats/
    json/
    xml/
  functions/
  ts-runtime/
  tests/
    fixtures/
    integration/
  examples/
    golden-path/
  .github/
    workflows/
```

This structure keeps artifacts, runtime code, format packs, docs site, and examples separated so work can be committed in small understandable pieces.[cite:3][cite:42][cite:147]

Tool source is `cli/`, `core/`, `formats/`, `functions/`, and `ts-runtime/`. Tool-test inputs are `tests/` and `spec/examples/`. `examples/golden-path/` is a reference **user project** (see "Tool repo vs user project") and follows the user project layout, not the tool layout.

## Documentation strategy

Documentation is part of the MVP, not a follow-up task. Docusaurus is the recommended framework because it is built for docs-as-code sites, supports built-in versioning, and works well with static hosting and open-source project workflows.[cite:130][cite:145][cite:137]

The initial docs deployment model should be:

- Framework: Docusaurus.[cite:145][cite:137]
- Source of truth: markdown and MDX files in the main repo.[cite:145][cite:147]
- Hosting: GitHub Pages using GitHub Actions-based deployment.[cite:150][cite:152]
- Versioning: Docusaurus docs versioning tied to releases.[cite:130][cite:133]
- Search: start with local search; add self-hosted Typesense later if stronger open-source search is needed.[cite:131][cite:149][cite:153]

The docs site should include:

- Getting started.
- Concepts.
- CLI reference.
- Config reference.
- Format packs.
- Testing guide.
- TypeScript transforms.
- Architecture.
- Contributing.
- Release notes.

## Docs maintenance guardrails

To keep docs up to date, the repo should enforce these rules:

- Every user-visible feature PR updates docs, examples, or explicitly records no docs impact.
- Every new CLI command or option updates CLI reference docs.
- Every format pack ships with its own docs and example fixtures.
- The golden-path example is referenced in docs and exercised in CI.
- Generated reference docs should come from schemas and CLI metadata when practical.
- Docs build must pass in CI before merge.[cite:147][cite:150]

The goal is to make docs drift visible immediately, not weeks later.

## Milestones

## M0 — Reboot foundation

### Deliverables

- Freeze the current repo state as a legacy reference branch or archive point.
- Create a reboot branch or clean repo structure.
- Write the product thesis, non-goals, and golden path.
- Add contribution rules for small commits and required tests.

### Exit criteria

- A new repo structure exists.
- `MVP_PLAN.md` is committed.
- The out-of-scope list is explicit.
- The first four milestones are written before implementation begins.

### Suggested commit slices

- `docs(mvp): define product thesis and non-goals`
- `chore(repo): create reboot folder structure`
- `docs(contrib): add commit and test workflow`

## M1 — Documentation platform and guardrails

### Deliverables

- Docusaurus site scaffolded in-repo.[cite:145]
- Initial information architecture and sidebar.
- GitHub Actions workflow to build and deploy docs to GitHub Pages.[cite:150][cite:152]
- Docs contribution rules and PR checklist.
- Placeholder pages for getting started, concepts, CLI, config, and contributing.

### Exit criteria

- Docs site builds locally.
- Docs site deploys automatically from the default branch.
- A PR can fail if docs build breaks.
- The repo has a visible docs update policy.[cite:147][cite:150]

### Suggested commit slices

- `feat(docs): scaffold docusaurus site`
- `docs(ia): define docs navigation and section layout`
- `ci(docs): build docs in github actions`
- `ci(docs): deploy docs to github pages`
- `docs(contrib): add docs update checklist`

## M2 — Config schema and validation

### Deliverables

- YAML project format version `v0alpha1`.
- JSON Schema definitions for config files.
- CLI command `weavster validate`.
- Human-friendly validation errors.
- Initial config reference page generated or written from the schema model.

### Exit criteria

- Valid config passes.
- Invalid config fails with path-aware errors.
- Validation can run in local dev and CI.[cite:49][cite:54]
- Docs explain the config structure and validation command.

### Suggested commit slices

- `docs(spec): define config v0alpha1`
- `test(schema): add valid and invalid config fixtures`
- `feat(validate): load yaml config`
- `feat(validate): validate config against schema`
- `refactor(validate): improve error formatting`
- `docs(validate): add validate command reference`

## M3 — Fixture test harness

### Deliverables

- Fixture file layout for input, expected output, and assertions.
- CLI command `weavster test`.
- Deterministic diff output for failures.
- Snapshot or structured output assertions.
- Docs page for local testing workflow.

### Exit criteria

- A developer can run local tests with one command.
- Failures show useful differences.
- Test cases can be added without touching runtime internals.[cite:101][cite:104]
- Docs include a first test example.

### Suggested commit slices

- `docs(testing): define fixture layout`
- `test(harness): add first failing fixture test`
- `feat(test): load fixture suite`
- `feat(test): compare actual and expected output`
- `refactor(test): improve diff readability`
- `docs(test): add local testing guide`

## M4 — Canonical document model

### Deliverables

- Internal normalized document representation.
- Path addressing rules.
- Metadata model for source format and validation errors.
- Concepts page describing the canonical model.

### Exit criteria

- JSON and XML inputs can both target the same internal model shape.
- Transform steps can operate against the canonical model rather than raw format-specific objects.[cite:70][cite:73][cite:74]
- Docs explain why this model exists.

### Suggested commit slices

- `docs(core): define canonical document model`
- `test(core): add canonical model mapping cases`
- `feat(core): create normalized node structures`
- `feat(core): add path access helpers`

## M5 — JSON format pack

### Deliverables

- JSON parser.
- JSON serializer.
- JSON input/output tests.
- Integration with canonical model.
- JSON format pack docs.

### Exit criteria

- JSON can be parsed, normalized, transformed, and emitted in tests.
- Docs show how JSON works in the golden path.

### Suggested commit slices

- `docs(format-json): define parser contract`
- `test(format-json): add round-trip fixtures`
- `feat(format-json): parse input`
- `feat(format-json): serialize output`

## M6 — XML format pack

### Deliverables

- XML parser.
- XML serializer.
- XML path mapping into canonical model.
- Basic XML validation abstraction, with room for XSD-backed validation later.[cite:71][cite:74]
- XML format pack docs.

### Exit criteria

- XML can complete the same golden path as JSON.
- XML-specific concerns do not leak into the transform DSL.
- Docs explain XML support and current limitations.

### Suggested commit slices

- `docs(format-xml): define parser contract`
- `test(format-xml): add round-trip fixtures`
- `feat(format-xml): parse input`
- `feat(format-xml): serialize output`
- `feat(format-xml): add validation interface`

## M7 — Declarative transform DSL

### Deliverables

- Minimal transform step language.
- Built-in operations such as map, rename, default, concat, conditionals, string helpers, and date helpers.
- Config-driven step execution.
- DSL reference docs and examples.

### Exit criteria

- Common transforms can be expressed without writing code.
- The golden-path example uses declarative steps only.[cite:55][cite:58]
- Docs include enough examples for a developer to copy and adapt.

### Suggested commit slices

- `docs(dsl): define core transform operations`
- `test(dsl): add operation fixtures`
- `feat(dsl): implement field mapping`
- `feat(dsl): implement string and default helpers`
- `feat(dsl): implement conditional logic`

## M8 — TypeScript escape hatch

### Deliverables

- Contract for custom TypeScript transform steps.
- Sandboxed or controlled execution model.
- Input and output contract validation around custom code.[cite:53][cite:56]
- TypeScript transform docs and examples.

### Exit criteria

- An advanced user can write one custom transform step in TypeScript.
- The custom step is optional and isolated from the declarative path.
- Contract failures surface as testable errors.
- Docs clearly describe when to use TypeScript instead of config.

### Suggested commit slices

- `docs(ts-runtime): define custom step contract`
- `test(ts-runtime): add custom transform fixtures`
- `feat(ts-runtime): execute custom step`
- `feat(ts-runtime): validate custom step I/O`

## M9 — Golden-path example and developer docs

### Deliverables

- One complete sample project.
- README quickstart.
- Docs walkthrough for init, validate, test, compile, and run.
- Architecture overview narrative.
- CI smoke test using the golden-path example.

### Exit criteria

- A new developer can clone the repo, run the example, and understand the product shape in under 30 minutes.
- The example is used in CI as a smoke test.
- The docs site teaches the same path the product expects users to follow.

### Suggested commit slices

- `docs(readme): add quickstart`
- `feat(example): add golden-path project`
- `docs(example): add walkthrough for golden path`
- `test(example): run golden-path in CI`

## Commit strategy

The reboot should favor small, reviewable changes. Research on small batches and trunk-based development emphasizes that smaller units reduce risk and improve feedback speed, and GitHub’s stacked PR workflow is explicitly designed for sequences of small dependent changes.[cite:97][cite:98][cite:101][cite:104]

Rules for commits:

- One commit should do one thing.
- Prefer a failing test commit before an implementation commit.
- Keep refactors separate from new behavior.
- Keep docs close to the feature they explain.
- Use conventional commits for structure and readability.[cite:105][cite:108]

Preferred sequence inside each feature slice:

1. Spec or docs.
2. Tests.
3. Minimal implementation.
4. Cleanup/refactor.
5. Example update.

## Pull request strategy

Pull requests should stay small enough to review quickly. Stacked PR workflows make this practical by letting a larger milestone land as a sequence of smaller dependent PRs rather than one giant branch.[cite:97][cite:99][cite:103]

PR rules:

- One PR per milestone slice, not one PR per milestone.
- Target roughly one concept per PR.
- Include passing tests or clearly marked failing-test-first groundwork only when intentional.
- Merge frequently into trunk or a short-lived integration branch.[cite:101][cite:104][cite:110]
- Docs changes should usually land in the same PR as behavior changes unless they are part of a separate docs-only cleanup.

## Testing strategy

Testing is part of the product, not a support activity. The MVP should ship with multiple layers of tests:

- Schema validation tests.
- Fixture-based transformation tests.
- Format round-trip tests.
- Golden-path integration tests.
- Contract tests for the TypeScript escape hatch.
- Docs build tests in CI.[cite:49][cite:54][cite:101][cite:150]

A new capability is not complete until its local test story and docs story are clear and repeatable.

## Documentation operations

The docs system should have its own lightweight operating model:

- Build docs on every pull request.
- Deploy docs automatically on merge to the default branch.[cite:150][cite:152]
- Cut versioned docs on releases using Docusaurus versioning.[cite:130][cite:133]
- Keep the number of actively surfaced docs versions intentionally limited.[cite:130]
- Add search later with Typesense when the docs volume justifies it.[cite:131][cite:149][cite:153]

## Definition of done

A milestone is done only when:

- The behavior is documented.
- Tests exist and pass locally.
- The CLI surface is understandable.
- An example or fixture demonstrates the behavior.
- The docs site builds successfully.
- The implementation does not introduce speculative abstraction.

## Recommended first 14 days

### Week 1

- Complete M0.
- Complete M1.
- Define `v0alpha1` config shape.
- Add schema fixtures.
- Implement `weavster validate`.
- Commit the first validation slices.

### Week 2

- Complete M2 and M3.
- Define fixture layout.
- Implement `weavster test`.
- Add one JSON-based golden-path fixture set.
- Ensure CI can run validation, tests, and docs build commands.

At the end of two weeks, the reboot should already have a real center of gravity: docs, config, validation, and tests. That foundation matters more than format breadth or runtime sophistication at this stage.[cite:101][cite:104][cite:147]

## Future after MVP

Once MVP is stable, the next likely expansions are:

- HL7 v2 format pack.[cite:70][cite:73]
- X12 format pack, with careful separation between parser/runtime support and any licensed standard artifacts.[cite:64][cite:65]
- Additional transports such as file, SFTP, REST, and TCP.
- Stronger contract packs such as XSD-based XML validation.[cite:74]
- Optional WASM plugin support for expert users.
- Machine-oriented docs index and MCP-friendly docs retrieval layer aligned to the same docs source tree.[cite:126][cite:4][cite:127]

These should only begin after the MVP golden path is solid, documented, and easy to test locally.
