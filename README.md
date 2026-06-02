# Weavster

Config-driven integration tool: define transformation pipelines in YAML, validate
them locally, test them with fixtures, and run them through a modular engine.

> Status: early reboot. The plan and direction live in
> [`docs/MVP_PLAN.md`](docs/MVP_PLAN.md); active tasks in
> [`docs/MVP_TASKS.md`](docs/MVP_TASKS.md).

## What exists today

- Repository scaffold and folder structure.
- Contribution rules ([`CONTRIBUTING.md`](CONTRIBUTING.md)) and PR template.
- Editor/formatter config (`.editorconfig`, Prettier).
- Dev log ([`notes/DEV_LOG.md`](notes/DEV_LOG.md)) and changelog
  ([`CHANGELOG.md`](CHANGELOG.md)).

No CLI, runtime, or format packs are implemented yet.

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
