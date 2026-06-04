# Weavster

Config-driven integration tool: define transformation pipelines in YAML, validate
them locally, test them with fixtures, and run them through a modular engine.

> Status: early reboot. The plan and direction live in
> [`docs/MVP_PLAN.md`](docs/MVP_PLAN.md); active tasks in
> [`docs/MVP_TASKS.md`](docs/MVP_TASKS.md).

## What exists today

- Repository scaffold and folder structure.
- Docusaurus documentation site in [`website/`](website/) with placeholder pages and
  CI to build on PRs and deploy to [docs.weavster.dev](https://docs.weavster.dev) on merge.
- pnpm workspace at the repo root.
- `weavster validate`: validates a project's `weavster.yaml` against the `v0alpha1`
  schema ([`spec/schemas/project.schema.json`](spec/schemas/project.schema.json)) with
  path-aware errors.
- `weavster test`: runs a project's `fixtures/` (input vs expected JSON) and prints a
  diff for any mismatch. The flow is an identity passthrough until the transform engine
  lands, so a fixture passes when `expected.json` matches `input.json`.
- A reference user project at [`examples/golden-path/`](examples/golden-path/) exercised
  by `validate` and `test`.
- `@weavster/core`: the canonical document model — a format-agnostic node tree
  (`scalar`/`object`/`array`) with `fromValue`/`toValue` normalization and dotted-path
  access helpers (`get`/`getValue`). See [Concepts](https://docs.weavster.dev/concepts).
- Contribution rules ([`CONTRIBUTING.md`](CONTRIBUTING.md)) and PR template.
- Editor/formatter config (`.editorconfig`, Prettier).
- Dev log ([`notes/DEV_LOG.md`](notes/DEV_LOG.md)) and changelog
  ([`CHANGELOG.md`](CHANGELOG.md)).

Format packs and the transform engine are not implemented yet; `validate` and `test`
exist of the planned CLI commands, and `@weavster/core` provides the model they will
build on.

## Local development

Requires Node 22+ and pnpm.

```bash
pnpm install        # install workspace dependencies
pnpm docs:start     # run the docs site locally
pnpm docs:build     # production build of the docs site
pnpm test           # run all package test suites (core + cli)
pnpm format         # format with Prettier

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
| `core/`       | Canonical document model and engine                        |
| `formats/`    | Format packs (JSON, XML)                                   |
| `functions/`  | Built-in transform functions                               |
| `ts-runtime/` | TypeScript escape hatch for custom transforms              |
| `tests/`      | Fixtures and integration tests                             |
| `examples/`   | Golden-path example project                                |

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Work proceeds in small, reviewable slices
following [`docs/MVP_TASKS.md`](docs/MVP_TASKS.md).

## License

See [`LICENSE`](LICENSE).
