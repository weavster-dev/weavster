# Release checklist — first MVP tag

A short, repeatable checklist for cutting the first Weavster MVP release.

## Scope of the MVP

Shipping:

- `@weavster/core`: canonical document model, path access, JSON + XML format packs, and the
  v0alpha2 transform engine (declarative DSL + `_ts` escape hatch).
- `@weavster/cli`: `weavster init`, `weavster validate`, `weavster test`.
- A reference example (`examples/golden-path/`) and the docs site.

Explicitly **not** in this MVP (tracked in the after-MVP backlog in `docs/archive/MVP_TASKS.md`):
`compile`/`run` commands, HL7/X12 packs, additional transports, XSD validation, the WASM
plugin path, and the Rust/WASM production runtime.

## Pre-tag checks

Run from a clean checkout:

- [ ] `pnpm install --frozen-lockfile` succeeds.
- [ ] `pnpm --filter @weavster/core build` and `pnpm cli:build` are clean.
- [ ] `pnpm test` passes (core + cli).
- [ ] `pnpm format:check` is clean.
- [ ] `pnpm docs:build` succeeds.
- [ ] Golden-path smoke: `node cli/dist/index.js validate examples/golden-path` and
      `… test examples/golden-path` both pass.
- [ ] Init smoke: `weavster init <tmp> && weavster validate <tmp> && weavster test <tmp>` pass.
- [ ] CI is green on `main` (the `ci` and `docs-build` workflows).

## Clean-machine confirmation

- [ ] Clone the repo fresh, run the [Getting Started](https://docs.weavster.dev/getting-started)
      steps end to end, and confirm a new developer reaches a passing `weavster test` quickly.

## Docs and notes

- [ ] `README.md` and `CHANGELOG.md` reflect the actual shipped surface.
- [ ] Docs site reads in order: Getting Started → Concepts → CLI → Config → Format Packs →
      Transform DSL → TypeScript Transforms → Testing → Architecture.
- [ ] `notes/DEV_LOG.md` has an entry for the release.

## Tag

- [ ] Move the `[Unreleased]` CHANGELOG section under the new version with a date, and set the
      new version in `cli/package.json` (and `core/package.json`).
- [ ] Confirm npm **trusted publishing** is configured for `@weavster/cli` (npmjs.com → package
      Settings → Trusted Publisher → GitHub `weavster-dev/weavster`, workflow `release.yml`). No
      npm token is used — publishing is OIDC-based.
- [ ] Create the version tag on `main` and push it: `git tag vX.Y.Z && git push origin vX.Y.Z`.
      The `release` workflow then builds, tests, publishes `@weavster/cli` to npm via OIDC, and
      creates a GitHub Release with notes from the matching `CHANGELOG.md` section.
