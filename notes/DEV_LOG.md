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
