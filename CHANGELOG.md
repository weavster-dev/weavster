# Changelog

All notable changes to this project are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- M2 config validation: `v0alpha1` project schema at `spec/schemas/project.schema.json`.
- `@weavster/cli` package with the `weavster validate` command (commander + Ajv + yaml),
  emitting path-aware error messages.
- Valid and invalid sample configs under `spec/examples/project/` and a vitest suite.
- `ci` GitHub Actions workflow that builds and tests the CLI on PRs and pushes to main.
- M0 reboot foundation: top-level folder structure from `MVP_PLAN.md`.
- `.gitignore`, `.editorconfig`, Prettier config (`.prettierrc.json`, `.prettierignore`).
- `CONTRIBUTING.md` with small-PR, testing, and docs-update rules.
- `.github/PULL_REQUEST_TEMPLATE.md` with docs and test checklist.
- `notes/DEV_LOG.md` work journal with entry template.
- `CHANGELOG.md` and a CLAUDE.md rule to update it with every commit.
- `README.md` describing the current state of the project.
- CLAUDE.md rule to keep `README.md` matching actual (not future) state.
- GitHub milestone labels M0–M9 matching the MVP plan.
- M1 documentation platform: Docusaurus site in `website/` (TypeScript, classic preset).
- pnpm workspace at the repo root with `docs:start` / `docs:build` / `docs:serve` / `format` scripts.
- Documentation IA: explicit sidebar and placeholder pages for Getting Started, Concepts,
  CLI, Config, Testing, Architecture, and Contributing.
- GitHub Actions: build docs on PRs (`docs-build`) and deploy to GitHub Pages on merge (`docs-deploy`).

### Changed

- Serve the docs site from the `docs.weavster.dev` custom domain (`CNAME` + `url`/`baseUrl`).
- Moved planning docs from `mvp/` to `docs/` to match the plan's repo shape.
- Clarified tool-repo vs user-project split in `MVP_PLAN.md`: defined the `weavster init` user-project layout once, separated tool-test fixtures from user-project fixtures, and pinned `examples/golden-path/` as the reference `init` output.
- Patched M9 in `MVP_TASKS.md` to generate the golden-path example via `weavster init` and treat it as the `init` output contract.
