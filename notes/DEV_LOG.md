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
