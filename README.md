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
- Contribution rules ([`CONTRIBUTING.md`](CONTRIBUTING.md)) and PR template.
- Editor/formatter config (`.editorconfig`, Prettier).
- Dev log ([`notes/DEV_LOG.md`](notes/DEV_LOG.md)) and changelog
  ([`CHANGELOG.md`](CHANGELOG.md)).

The runtime and format packs are not implemented yet, and only `validate` exists of
the planned CLI commands.

## Local development

Requires Node 22+ and pnpm.

```bash
pnpm install        # install workspace dependencies
pnpm docs:start     # run the docs site locally
pnpm docs:build     # production build of the docs site
pnpm test           # run the CLI test suite
pnpm format         # format with Prettier

# validate a project during development
pnpm --filter @weavster/cli dev validate ./path/to/project
```

## Layout

| Path | Purpose |
| --- | --- |
| `docs/` | Plan, task list, and (later) the documentation site source |
| `website/` | Docusaurus docs site (not yet scaffolded) |
| `spec/` | Config JSON Schemas and example configs |
| `cli/` | CLI commands |
| `core/` | Canonical document model and engine |
| `formats/` | Format packs (JSON, XML) |
| `functions/` | Built-in transform functions |
| `ts-runtime/` | TypeScript escape hatch for custom transforms |
| `tests/` | Fixtures and integration tests |
| `examples/` | Golden-path example project |

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Work proceeds in small, reviewable slices
following [`docs/MVP_TASKS.md`](docs/MVP_TASKS.md).

## License

See [`LICENSE`](LICENSE).
