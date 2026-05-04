# Weavster

**Current status:** early MVP. Weavster can run local file-based JSONL flows through generated WASM transforms. Several connector, routing, packaging, and management surfaces are present in the codebase but are partial, config-only, placeholder, or planned.

[![CI](https://github.com/weavster-dev/weavster/actions/workflows/ci.yml/badge.svg)](https://github.com/weavster-dev/weavster/actions/workflows/ci.yml)

## What is Weavster?

Weavster is a developer tool for defining data transformation flows in YAML and running them through a WASM-backed runtime. The current usable path is local development with file connectors, JSONL input/output, and a small set of transforms.

Longer term, Weavster is intended to grow toward an event-driven integration runtime with richer connectors, routing, packaging, and deployment workflows. Those future goals are documented below as planned work, not current runtime guarantees.

## Status Labels

| Label | Meaning |
| --- | --- |
| Current | Usable today in the documented workflow |
| Partial | Implemented in some layers but limited or incomplete |
| Config-only | Parsed or modeled, but not executed end-to-end |
| Placeholder | Command or surface exists but does not provide useful behavior yet |
| Planned | Roadmap item with no current usable behavior |

## Quick Start

### Prerequisites

- Rust 1.92.0 or newer, matching the workspace `rust-version`
- `wasm32-unknown-unknown` target available through rustup

### Install from source

```bash
git clone https://github.com/weavster-dev/weavster.git
cd weavster
cargo install --path crates/weavster-cli
```

### Create and run the generated example

```bash
weavster init my-project
cd my-project
weavster run
```

The generated project includes:

```text
my-project/
├── weavster.yaml
├── profiles.yaml
├── flows/
│   └── example_flow.yaml
├── connectors/
│   └── file.yaml
├── tests/
└── data/
    └── input.jsonl
```

The current example reads `data/input.jsonl`, applies `map`, `drop`, and `add_fields`, and writes `data/output.jsonl`.

## Current Example Flow

```yaml
# flows/example_flow.yaml
name: example_flow
description: An example flow to get you started

input: file.input

transforms:
  - map:
      full_name: name
      email: email

  - drop:
      - name
      - age

  - add_fields:
      processed: true

outputs:
  - file.output
```

Connector references use the `filename.key` form. For example, `file.input` resolves to the `input` entry in `connectors/file.yaml`.

```yaml
# connectors/file.yaml
input:
  type: file
  path: "./data/input.jsonl"
  format: jsonl

output:
  type: file
  path: "./data/output.jsonl"
  format: jsonl
```

## Feature Status

### CLI

| Command | Status | Notes |
| --- | --- | --- |
| `weavster init` | Current | Creates a local file-based starter project |
| `weavster run` | Current | Compiles flows to WASM and runs file-based flows |
| `weavster compile` | Current | Compiles flow YAML to cached WASM artifacts |
| `weavster test` | Partial | Runs YAML-defined flow tests through compiled WASM where available |
| `weavster validate` | Partial | Loads project config; deeper flow, connector, and expression validation is still TODO |
| `weavster package` | Partial | Creates an artifact directory; manifest digest and signing are incomplete |
| `weavster status` | Placeholder | Command exists but prints "Not yet implemented" |
| `weavster flow ...` | Placeholder | Subcommands exist but are not implemented |
| `weavster connector ...` | Placeholder | Subcommands exist but are not implemented |
| `weavster push` / `weavster pull` | Planned | Not currently CLI commands |

### Runtime and State

| Area | Status | Notes |
| --- | --- | --- |
| File input/output runtime | Current | JSONL file processing is the supported end-to-end path |
| Local state store | Current | Uses SQLite at `.weavster/data/local.db` |
| Postgres state store | Partial | Available when `WEAVSTER_PG_URL` is set |
| Remote distributed runtime | Planned | Redis-backed distributed mode is not implemented |
| Continuous polling/watch mode | Planned | Current file flow runs over available input records and exits |

### Connectors

| Connector | Status | Notes |
| --- | --- | --- |
| File | Current | Runtime supports file input/output; JSONL is the exercised path |
| Kafka | Config-only | Config structs parse, but runtime execution rejects non-file connectors |
| PostgreSQL | Config-only | Config structs parse; runtime connector I/O is not implemented |
| HTTP | Config-only | Config structs parse; runtime connector I/O is not implemented |
| Bridge | Config-only | Config structs parse; runtime bridge processing is not implemented |

### Transforms

| Transform | Status | Notes |
| --- | --- | --- |
| `map` | Current | Supported by the generated example and direct interpreter |
| `drop` | Current | Supported by the generated example and direct interpreter |
| `add_fields` | Current | Supported by the generated example and direct interpreter |
| `coalesce` | Partial | Parsed, interpreted, and code-generated; not in the starter flow |
| `regex` | Partial | Parsed and code-generated; not supported by the direct interpreter |
| `template` | Partial | Parsed and code-generated; not supported by the direct interpreter |
| `lookup` | Partial | Parsed and code-generated, but lookup data is not loaded into generated artifacts in the normal CLI path |
| `filter` | Partial | Parsed, but generated runtime behavior is currently pass-through/incomplete |
| Conditional outputs | Partial | Parsed, but current runtime delivery sends each successful record to all configured outputs |

### Configuration

| Area | Status | Notes |
| --- | --- | --- |
| `weavster.yaml` project config | Current | Supports project metadata, runtime config, vars, profiles, macros dir, and error handling |
| `flows/*.yaml` | Current | Loads connector references, transforms, outputs, vars, and flow-level error handling |
| `connectors/*.yaml` | Current for parsing | Connector references resolve as `filename.key` |
| `macros/*.yaml` | Current | Macro definitions can expand reusable transform sequences |
| Static Jinja vars/env | Current | Known config-level expressions are substituted at load time |
| Runtime Jinja helpers | Partial | Dynamic helpers such as `now()`, `uuid()`, and `timestamp()` are limited to supported transform paths |
| Generated `profiles.yaml` | Planned alignment | `weavster init` writes this file, but current config loading expects profiles inside `weavster.yaml` |

## Known Limitations

- Runtime connector execution is file-only today.
- Local state uses SQLite, not embedded PostgreSQL.
- `weavster run` has no one-shot or per-flow mode flags; use `weavster test` for explicit one-shot flow checks.
- `status`, `flow`, and `connector` management commands are placeholders.
- `validate` does not yet validate all flows, connector references, or expressions.
- Packaging does not yet produce a complete OCI registry workflow, and signing is not implemented.
- Coverage is generated in CI, but local coverage tooling is not assumed to be installed.

## Roadmap

### MRK: Mapping, Routing, and Keys

MRK is planned as the next major direction for richer message orchestration:

- **Mapping**: Dynamic field mapping across multiple schemas
- **Routing**: Event-driven routing logic beyond the current file flow path
- **Keys**: Partitioning and key management for distributed streams

### Planned Runtime Work

- Kafka, PostgreSQL, HTTP, and Bridge connector execution
- Real conditional routing and output filtering
- Remote/distributed runtime support
- OCI registry push/pull and signing workflow
- Flow and connector management commands

## Development

### Build and verify

```bash
cargo build
cargo test --all-features
cargo clippy --all-targets --all-features -- -D warnings
RUSTDOCFLAGS="-D warnings" cargo doc --no-deps --all-features
```

The local audit for this documentation pass found:

- `cargo test --all-features` passes when generated WASM crates can resolve dependencies.
- `cargo fmt --check` passes.
- `cargo clippy --all-targets --all-features -- -D warnings` passes.
- `RUSTDOCFLAGS="-D warnings" cargo doc --no-deps --all-features` passes.
- CI runs coverage through `cargo llvm-cov`, but `cargo llvm-cov` was not installed in the audited local environment.
- The CI coverage gate currently accepts 60%, while repository agent instructions call for 90% unit test coverage. That mismatch is documented here and not changed by this documentation update.

### Project Structure

```text
crates/
├── weavster-core/      # Config, flow model, connector traits/configs, transforms, interpreter
├── weavster-codegen/   # YAML to IR to Rust/WASM generation
├── weavster-runtime/   # WASM execution runtime and state stores
└── weavster-cli/       # Developer CLI
```

Future Python bindings are noted in the workspace, but `weavster-python` is not currently a workspace crate.

## Documentation Status

The top-level README is the current status baseline. The Docusaurus docs under `docs/docs/` should use the same status labels and should not present planned or placeholder behavior as current.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Weavster is licensed under the [Business Source License 1.1](LICENSE) (BSL 1.1).

**What this means:**

- **Free for most uses**: You can use, modify, and distribute Weavster for any purpose that does not compete with our paid offerings
- **Source available**: Full source code is always available
- **Converts to open source**: Each version automatically converts to [MPL 2.0](https://www.mozilla.org/en-US/MPL/2.0/) after 4 years
- **Free products exempt**: Products offered free of charge are never considered competitive

**Not permitted** without a commercial license:

- Offering Weavster as a hosted service that competes with Weavster Dev's paid products
- Embedding Weavster in a competing commercial product

For commercial licensing inquiries, see the [LICENSE](LICENSE) file or contact us.
