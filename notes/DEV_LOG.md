# Dev Log

Newest entries on top. One entry per merged slice.

## Template

```
## YYYY-MM-DD — <slice name>
- What changed:
- What I learned:
- What is next:
```

---

## 2026-06-03 — M4 canonical document model

- What changed: Created the `@weavster/core` package holding the canonical model.
  `core/src/model.ts` defines `Node` as a tagged union of `scalar`/`object`/`array`, a
  `Document` wrapping a root node with `{ sourceFormat, errors }` metadata, type guards,
  and `fromValue`/`toValue` to normalize native JS values to/from nodes.
  `core/src/path.ts` defines path addressing: segment arrays are canonical (strings =
  object fields, numbers = array indices), with `parsePath`/`formatPath` for the dotted +
  bracket string form (`lines[0].sku`) and `get`/`getValue` to resolve a path to a node or
  value. Added the package to the pnpm workspace, switched root `test` to `pnpm -r test`,
  added a core build step to CI, and wrote the Concepts page.
- What I learned: The model is the seam that lets one transform serve many formats — by
  the time a transform runs, format is gone and only nodes remain (M5 JSON / M6 XML both
  target the same three kinds). Decisions: a tagged union (not native values + sidecar
  metadata) makes XML attributes/text/order representable later without reshaping; the
  dotted+bracket path syntax keeps a numeric object key (`counts.0`, string) distinct from
  an array index (`counts[0]`, number). `fromValue`/`toValue` are the model's intake
  boundary; format packs own only text⇄value, the model owns value⇄node. vitest runs TS
  without typechecking, so CI builds core with `tsc` to catch type errors.
- What is next: M5 — JSON format pack (parse/serialize, map into the canonical model).

## 2026-06-03 — M3 fixture test harness

- What changed: Added `weavster test [path]`. `cli/src/fixtures.ts` scans a project's
  `fixtures/` directory (one folder per case, each with `input.json` + `expected.json`),
  runs each input through `runFlow`, deep-compares the result to expected, and builds a
  line-by-line JSON diff on mismatch. `cli/src/commands/test.ts` prints `✓`/`✗` per case,
  a passed count, and sets exit code 1 on any failure. Created `examples/golden-path/`, a
  real user project (matching the `weavster init` layout) used as a CI smoke test, plus
  tool-test fixtures under `tests/fixtures/harness/` (passing + failing). Wrote the testing
  guide and `test` CLI docs.
- What I learned: M3 has no transform engine, so `runFlow` is an identity passthrough —
  output equals input, and a fixture passes when `expected.json` matches `input.json`. The
  harness is deliberately decoupled from the flow: M4–M6 swap the body of `runFlow` for the
  canonical model + transform DSL without touching loader, compare, or diff. The data flow
  is path → `fixtures/` scan → per-case parse → `runFlow` → `deepEqual` → diff. Keeping
  "tool-test fixtures" (`tests/fixtures/`, verify the tool) separate from "user-project
  fixtures" (a project's `fixtures/`, verified by `weavster test`) avoids confusion.
- What is next: M4 — canonical document model.

## 2026-06-02 — M2 config schema and validation

- What changed: Defined the `v0alpha1` project schema (`spec/schemas/project.schema.json`):
  required `apiVersion` (const `weavster/v0alpha1`) and `name` (kebab pattern), optional
  `description`, and `additionalProperties: false`. Added the `@weavster/cli` package with
  `weavster validate [path]` — resolves `weavster.yaml`, parses YAML, validates with Ajv,
  and prints path-aware errors. Added valid + 4 invalid sample configs, a vitest suite, and
  a `ci` workflow.
- What I learned: A schema-failing config: `name: Orders To Warehouse` fails the
  `^[a-z0-9][a-z0-9-]*$` pattern (spaces and uppercase not allowed). Schema validation here is
  shape/type checking only — it cannot catch deeper problems like a flow referencing a field
  that does not exist; that is compile-time validation, which comes in later milestones.
  Ajv v8 needs the named import `{ Ajv }` to construct cleanly under TypeScript NodeNext.
- What is next: M3 — fixture test harness (`weavster test`).

## 2026-06-02 — M1 documentation platform

- What changed: Scaffolded a Docusaurus TypeScript site in `website/`, wired the repo
  as a pnpm workspace (root `package.json` + `pnpm-workspace.yaml`), set Weavster config
  (title, GitHub Pages URL/baseUrl, nav, footer, blog disabled), replaced the sample
  tutorial content with an explicit sidebar and 7 placeholder pages, and added two CI
  workflows: `docs-build` (PRs) and `docs-deploy` (GitHub Pages on merge to main).
- What I learned: Docs are built with `pnpm docs:build` (delegates to `docusaurus build`
  in `website/`). Nav/footer live in `website/docusaurus.config.ts`, page order in
  `website/sidebars.ts`, pages in `website/docs/*.md`. Deploy publishes `website/build`
  via `upload-pages-artifact` + `deploy-pages`; GitHub Pages must be set to "GitHub Actions"
  as its source in repo settings for the deploy job to succeed.
- What is next: M2 — config schema and validation (`weavster validate`).

## 2026-06-02 — M0 reboot foundation

- What changed: Added `.gitignore`, `.editorconfig`, Prettier config, `CONTRIBUTING.md`,
  PR template, and `notes/DEV_LOG.md`. Created the top-level folder structure from
  `MVP_PLAN.md` and moved the planning docs into `docs/`.
- What I learned: Repo was effectively greenfield (only `CLAUDE.md` + `LICENSE` tracked),
  so no legacy code to freeze. New direction is a Node/TS stack per the plan.
- What is next: M1 — scaffold the Docusaurus site in `website/` and wire up docs CI.
