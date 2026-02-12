# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with this repository.

**Project**: Weavster
**Description**: Modern Enterprise Service Bus - like dbt but for real-time transactions
**Tech Stack**: Rust, WASM (wasmtime), PostgreSQL (postgresql_embedded), MiniJinja

## Project Vision

Weavster is a developer-friendly tool for building real-time data transformation pipelines:
- **dbt-like DX**: YAML configuration, Jinja templating, simple CLI
- **Real-time focus**: FIFO queues, not batch processing
- **Zero-config local dev**: Embedded PostgreSQL, single binary distribution
- **Safe transforms**: WASM sandboxing for untrusted code (escape hatch only)

## Quick Start

```bash
# Build all crates
cargo build

# Run tests
cargo test

# Run the CLI
cargo run -p weavster-cli -- --help

# Run with local embedded Postgres
cargo run -p weavster-cli -- run

# Format code
cargo fmt

# Lint
cargo clippy -- -D warnings
```

## Workspace Structure

This is a Cargo workspace with multiple crates:

| Crate | Purpose | Depends On |
|-------|---------|------------|
| `weavster-core` | Shared library: config, transforms, connectors | - |
| `weavster-runtime` | Execution engine for Docker deployment | `weavster-core` |
| `weavster-cli` | Developer CLI tool | `weavster-core`, `weavster-runtime` |
| `weavster-python` | PyO3 bindings for Python ecosystem | `weavster-core` |

### Dependency Rules

- `weavster-core` has NO dependencies on other workspace crates
- `weavster-runtime` depends ONLY on `weavster-core`
- `weavster-cli` can depend on any workspace crate
- Keep `weavster-runtime` minimal for small Docker images

## Architecture

### Core Concepts

- **Project**: Collection of flows organized in a directory with `weavster.yaml`
- **Flow**: 1 input → N outputs with transforms in between
- **Connector**: Input/Output adapter (Kafka, Postgres, HTTP, File, etc.)
- **Bridge**: Special connector linking flows together
- **Transform**: Data manipulation (rename, filter, compute, etc.)

### Transform Pipeline

```
YAML Config → Parse → Rust Codegen → WASM Compile → Execute (wasmtime)
```

**All transforms compile to WASM.** The YAML transform DSL is parsed and translated
into Rust code, which is then compiled to WASM. This provides:

- **Sandboxed execution** - Transforms can't escape the WASM sandbox
- **Portable artifacts** - Same WASM runs anywhere wasmtime runs
- **Supply chain security** - OCI artifacts can be signed and verified

Transform types:
- **Simple mappings** - Direct field assignments (zero overhead)
- **Regex** - Pattern matching via `regex` crate compiled into WASM
- **Jinja templates** - `minijinja` compiled into WASM
- **Lookups** - Translation tables embedded as `phf` static maps

### Local Development Runtime

```
weavster run
    │
    ├── Start postgresql_embedded (if local mode)
    ├── Run migrations (apalis tables)
    ├── Parse weavster.yaml + flows/*.yaml
    ├── Render Jinja templates
    ├── Start connector workers
    └── Process messages through flows
```

### Configuration Hierarchy

```
weavster.yaml           # Project config, runtime settings
├── flows/
│   ├── flow_a.yaml     # Individual flow definitions
│   └── flow_b.yaml
├── connectors/
│   └── kafka.yaml      # Reusable connector configs
└── profiles.yaml       # Environment-specific overrides (like dbt)
```

## Coding Standards

### Rust Style

- Follow Rust API Guidelines: https://rust-lang.github.io/api-guidelines/
- Use `cargo fmt` before committing (enforced by CI)
- Zero `clippy` warnings (enforced by CI with `-D warnings`)
- Prefer `thiserror` for library errors, `anyhow` for application errors
- Use `tracing` for logging, not `println!` or `log`

### Automatic Formatting with Claude Code

Files are automatically formatted after Claude edits them via PostToolUse hooks configured in `.claude/settings.json`:

- **cargo fmt** formats: Rust files (*.rs)
- **taplo** formats: TOML files (*.toml)
- **prettier** formats: YAML files (*.yaml, *.yml)

### Error Handling

```rust
// In weavster-core (library code)
use thiserror::Error;

#[derive(Error, Debug)]
pub enum ConfigError {
    #[error("failed to parse YAML: {0}")]
    ParseError(#[from] serde_yaml::Error),
    #[error("invalid flow configuration: {message}")]
    InvalidFlow { message: String },
}

// In weavster-cli (application code)
use anyhow::{Context, Result};

fn main() -> Result<()> {
    let config = load_config()
        .context("failed to load weavster.yaml")?;
    Ok(())
}
```

### Testing

- Unit tests in same file as code (`#[cfg(test)]` module)
- Integration tests in `tests/` directory
- Use `rstest` for parameterized tests
- Use `testcontainers` for integration tests needing real services
- Fixtures in `tests/fixtures/`

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use rstest::rstest;

    #[rstest]
    #[case("simple.yaml", true)]
    #[case("invalid.yaml", false)]
    fn test_config_parsing(#[case] file: &str, #[case] should_succeed: bool) {
        // ...
    }
}
```

### Documentation

- All public APIs must have doc comments
- Include examples in doc comments where helpful
- Update CHANGELOG.md for user-facing changes
- Documentation is part of "done" for all features and fixes

## Documentation Site

User-facing documentation lives in `/docs` (Docusaurus) and is published to https://docs.weavster.dev

### Structure

```
docs/
├── docs/                    # Markdown source files
│   ├── index.md             # Homepage
│   ├── getting-started/     # Installation, first flow
│   ├── configuration/       # Project, flows config
│   ├── concepts/            # Transforms, connectors
│   └── cli/                 # CLI reference
├── docusaurus.config.ts     # Site configuration
└── sidebars.ts              # Sidebar structure
```

### Versioning

- `next` - Built from `main` branch (development docs)
- `X.Y.Z` - Snapshot created on each release

### Local Development

```bash
cd docs
npm install
npm start           # Dev server at localhost:3000
npm run build       # Production build
```

### Deployment

Docs are automatically deployed via GitHub Actions:
- **Push to main** → Deploys `next` version
- **Release published** → Creates version snapshot and deploys

### Writing Docs

When adding features or making changes:
1. Update relevant docs in `/docs/docs/`
2. Follow existing patterns for YAML examples
3. Include practical examples where helpful
4. PR template includes documentation checklist

## Git & Version Control Rules

**STRICT REQUIREMENTS:**

- NEVER create commits without explicit user permission
- NEVER stage files without user confirmation
- NEVER push changes unless explicitly instructed
- ALWAYS wait for confirmation before any git operation
- When creating PRs, describe only the changes (no test/deploy plans)
- Use conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`

### YAGNI (You Aren't Gonna Need It)

We strongly value applying YAGNI principles:

- Don't create methods, scopes, or features in anticipation of future use
- Remove unused code rather than keeping it "just in case"
- Prefer inline code over abstractions until patterns emerge through actual usage
- Wait to extract concerns/services until there's a clear need
- Avoid premature optimization - add indexes, caching, etc. only when performance issues arise
- Keep implementations simple and direct until complexity is actually required

## Key Design Decisions

### Why Rust?
- Single binary distribution (like dbt)
- Memory safety without GC
- Excellent WASM compilation (both host and target)
- PyO3 for Python bindings
- Performance for real-time processing

### Why WASM for all transforms?
- Security: Sandboxed execution, no escape to host
- Portability: Same binary runs anywhere
- Supply chain: OCI artifacts can be signed
- Performance: Near-native speed via wasmtime
- Embedding: regex, minijinja, phf all compile cleanly to WASM

### Why OCI for packaging?
- Industry standard for artifact distribution
- Signing via cosign/sigstore
- No OS attack surface (FROM scratch)
- Works with existing registry infrastructure
- Versioning and immutability built-in

### Why postgresql_embedded?
- Zero-config local development
- Same semantics as production Postgres
- No Docker required for local dev
- Bundled feature includes PG in binary

### Why apalis for job queue?
- Native Postgres support (matches our embedded story)
- Async-native (tokio)
- Can swap to Redis for distributed deployments

### Why MiniJinja?
- Created by Armin Ronacher (Flask/Jinja creator)
- Familiar to dbt users
- Pure Rust, compiles to WASM
- No Python dependency

## Compile & Package Pipeline

### Transform Compilation

```bash
weavster compile                    # YAML → Rust → WASM for all flows
weavster compile --flow myflow      # Single flow
weavster compile --debug            # Output generated Rust for inspection
```

The compile process:
1. Parse `flows/*.yaml` and `weavster.yaml`
2. For each flow, generate Rust source code
3. Embed artifacts (translation tables, regex patterns) as static data
4. Compile to `wasm32-wasi` target
5. Cache in `.weavster/cache/`

### OCI Packaging

```bash
weavster package                    # Create OCI artifact
weavster package --sign             # Create and sign with cosign
weavster push <registry>            # Push to OCI registry
weavster pull <registry>            # Pull from registry
```

OCI artifact structure (FROM scratch - no OS):
```
manifest.json
├── flow.wasm                       # Compiled transform
├── config/
│   └── flow.yaml                   # Original config (reference)
├── artifacts/
│   ├── translations/*.csv          # Lookup tables
│   └── patterns/*.json             # Regex patterns
└── signatures/
    └── cosign.sig                  # Optional signature
```

### Codegen Architecture

```
crates/weavster-codegen/
├── src/
│   ├── lib.rs                      # Public API
│   ├── parser.rs                   # YAML → IR
│   ├── ir.rs                       # Intermediate representation
│   ├── generator.rs                # IR → Rust source
│   ├── compiler.rs                 # Rust → WASM
│   └── transforms/
│       ├── mod.rs
│       ├── map.rs                  # Field mapping codegen
│       ├── regex.rs                # Regex codegen
│       ├── template.rs             # Jinja codegen
│       └── lookup.rs               # Translation table codegen
└── templates/
    └── transform.rs.tmpl           # Rust code template
```

## File Organization

### Adding a New Connector

1. Create `crates/weavster-core/src/connectors/{name}.rs`
2. Implement `Connector` trait
3. Register in `crates/weavster-core/src/connectors/mod.rs`
4. Add config parsing in `crates/weavster-core/src/config/connectors.rs`
5. Add tests in `crates/weavster-core/tests/connectors/{name}.rs`
6. Document in `docs/docs/concepts/connectors.md`

### Adding a New Transform

1. Create `crates/weavster-core/src/transforms/{name}.rs`
2. Implement `Transform` trait
3. Register in transform DSL parser
4. Add tests with fixture YAML files
5. Document in `docs/docs/concepts/transforms.md`

## Dependencies Policy

### Allowed Categories

- **Serialization**: serde, serde_yaml, serde_json
- **Async Runtime**: tokio
- **Database**: sqlx, postgresql_embedded
- **Job Queue**: apalis
- **CLI**: clap
- **Templating**: minijinja (compiles to WASM)
- **Regex**: regex (compiles to WASM)
- **Static Maps**: phf (for lookup tables in WASM)
- **WASM Runtime**: wasmtime
- **WASM Compilation**: cargo + wasm32-wasi target
- **OCI/Registry**: oci-distribution, oras
- **Signing**: sigstore
- **Error Handling**: thiserror, anyhow
- **Logging**: tracing, tracing-subscriber
- **Code Generation**: quote, syn (for proc-macros if needed)
- **Testing**: rstest, testcontainers

### Adding New Dependencies

- Check for maintenance status and security
- Prefer pure Rust over C bindings when possible
- Consider binary size impact for `weavster-runtime`
- Document why the dependency is needed

## CI/CD

### Pull Request Checks

1. `cargo fmt --check` - Formatting
2. `cargo clippy -- -D warnings` - Linting
3. `cargo test` - All tests
4. `cargo doc --no-deps` - Documentation builds
5. Claude Code review - Automated feedback

### Release Process

1. Update version in workspace `Cargo.toml`
2. Update CHANGELOG.md
3. Create git tag `v{version}`
4. CI builds and publishes:
   - GitHub Release with binaries
   - Docker images to GHCR
   - crates.io (future)
   - PyPI via maturin (future)

## Common Tasks

### Running a Single Flow for Testing

```bash
cargo run -p weavster-cli -- run --flow flows/my_flow.yaml --once
```

### Debugging Transform Execution

```bash
RUST_LOG=weavster_core::transforms=debug cargo run -p weavster-cli -- run
```

### Testing with Real Kafka

```bash
docker-compose -f docker/docker-compose.test.yml up -d
cargo test --features integration-tests
```

### Building Minimal Runtime Image

```bash
docker build -f docker/Dockerfile.runtime -t weavster-runtime .
# Results in ~50MB image with just the runtime binary
```

## Troubleshooting

### Embedded Postgres Won't Start

```bash
# Clear cached Postgres data
rm -rf .weavster/data
# Retry
cargo run -p weavster-cli -- run
```

### Clippy False Positives

```rust
#[allow(clippy::specific_lint)]  // Add justification comment
```

### Slow Compilation

```bash
# Use mold linker (Linux)
RUSTFLAGS="-C link-arg=-fuse-ld=mold" cargo build

# Or sccache
RUSTC_WRAPPER=sccache cargo build
```

## External Resources

- [Rust API Guidelines](https://rust-lang.github.io/api-guidelines/)
- [apalis Documentation](https://docs.rs/apalis)
- [postgresql_embedded](https://docs.rs/postgresql_embedded)
- [MiniJinja](https://docs.rs/minijinja)
- [wasmtime](https://docs.wasmtime.dev/)
