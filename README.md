# Weavster

Config-driven integration tool: define transformation pipelines in YAML, validate
them locally, test them with fixtures, and run them through a modular engine.

> Status: early reboot. The plan and direction live in
> [`docs/MVP_PLAN.md`](docs/MVP_PLAN.md); active tasks in
> [`docs/MVP_TASKS.md`](docs/MVP_TASKS.md).

## Quickstart

Install the CLI from npm:

```bash
npm install -g @weavster/cli

weavster init my-integration  # scaffold a project
cd my-integration
weavster validate             # check config + flows
weavster test                 # run fixtures through flows
```

Or work from a clone of this repo (for development):

```bash
pnpm install
pnpm cli:link                 # builds the CLI and links `weavster` globally
```

See the [Getting Started guide](https://docs.weavster.dev/getting-started) for the first
30 minutes, including editing a transform.

## What exists today

- Repository scaffold and folder structure.
- Docusaurus documentation site in [`website/`](website/) with placeholder pages and
  CI to build on PRs and deploy to [docs.weavster.dev](https://docs.weavster.dev) on merge.
- pnpm workspace at the repo root.
- `weavster init [dir]`: scaffolds a minimal starter project (config, a flow, a fixture,
  README) that passes `weavster test` out of the box.
- `weavster validate`: validates a project's `weavster.yaml` against the `v0alpha2`
  schema ([`spec/schemas/project.schema.json`](spec/schemas/project.schema.json)) and each
  `flows/*.yaml` against the flow schema, with path-aware errors.
- `weavster test`: runs each fixture (`fixtures/<flow>/<case>/`) through its
  `flows/<flow>.yaml` and prints a diff for any mismatch against `expected.json`.
- A reference user project at [`examples/golden-path/`](examples/golden-path/) exercised
  by `validate` and `test`.
- `@weavster/core`: the canonical document model — a format-agnostic node tree
  (`scalar`/`object`/`array`) with `fromValue`/`toValue` normalization and dotted-path
  access helpers (`get`/`getValue`). See [Concepts](https://docs.weavster.dev/concepts).
- JSON and XML format packs (`@weavster/core` `json` / `xml` namespaces):
  `parse`/`serialize` between text and the canonical model with stable round-tripping. The
  XML pack (fast-xml-parser) maps attributes to `@`-fields, text to `#text`, and repeated
  elements to arrays, plus a pluggable `XmlValidator`. See
  [Format Packs](https://docs.weavster.dev/formats).
- Transform engine (`@weavster/core` `applyFlow`): a `v0alpha2` patch-by-default pipeline over
  the canonical model. Steps are single-key `_op` operators (`_set`/`_default`/`_unset`/
  `_rename`/`_append`/`_select`/`_when`/`_ts`); values are expressions with `$path` references
  and `_op` operators (`_concat`, `_upper`, `_toIso`, `_eq`, `_cond`, …). Driven from
  `flows/*.yaml` via `weavster test`. See [Transform DSL](https://docs.weavster.dev/dsl).
- TypeScript escape hatch (`_ts` step): runs a custom `functions/<module>.ts` (pure JSON in/out,
  loaded via jiti) when the declarative DSL isn't enough. See
  [TypeScript Transforms](https://docs.weavster.dev/typescript).
- Contribution rules ([`CONTRIBUTING.md`](CONTRIBUTING.md)) and PR template.
- Editor/formatter config (`.editorconfig`, Prettier) and Biome linter (`biome.json`).
- CodeRabbit reviews configured in `.coderabbit.yaml`: the high-level summary is written into the
  PR description, replacing the `@coderabbitai summary` placeholder the PR template ends with
  (under a `---`). The config also enables the Biome linter and other tools relevant to this repo.
- Dev log ([`notes/DEV_LOG.md`](notes/DEV_LOG.md)) and changelog
  ([`CHANGELOG.md`](CHANGELOG.md)).

The transform engine is wired into the CLI: `weavster test` runs project flows over their
fixtures. `init`, `validate`, and `test` are the working CLI commands; `compile` and `run`
are still planned.

## Local development

Requires Node 22+ and pnpm.

```bash
pnpm install        # install workspace dependencies
pnpm docs:start     # run the docs site locally
pnpm docs:build     # production build of the docs site
pnpm test           # run all package test suites (core + cli)
pnpm format         # format with Prettier
pnpm lint           # lint with Biome

# run a command against a project during development
pnpm --filter @weavster/cli dev validate ./path/to/project
pnpm --filter @weavster/cli dev test ./path/to/project
```

## Layout

| Path          | Purpose                                                    |
| ------------- | ---------------------------------------------------------- |
| `docs/`       | Plan, task list, and (later) the documentation site source |
| `website/`    | Docusaurus docs site (not yet scaffolded)                  |
| `spec/`       | Config JSON Schemas and example configs                    |
| `cli/`        | CLI commands                                               |
| `core/`       | Canonical document model, format packs, and engine         |
| `formats/`    | Reserved for format packs if later extracted from `core/`  |
| `functions/`  | Built-in transform functions                               |
| `ts-runtime/` | TypeScript escape hatch for custom transforms              |
| `tests/`      | Fixtures and integration tests                             |
| `examples/`   | Golden-path example project                                |

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Work proceeds in small, reviewable slices
following [`docs/MVP_TASKS.md`](docs/MVP_TASKS.md).

## License

See [`LICENSE`](LICENSE).
