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

For architecture details and design decisions, see `plans/architecture-and-design.md`.

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
| `weavster-codegen` | Transform compilation: YAML → Rust → WASM | `weavster-core` |
| `weavster-runtime` | Execution engine for Docker deployment | `weavster-core` |
| `weavster-cli` | Developer CLI tool | `weavster-core`, `weavster-runtime` |

### Dependency Rules

- `weavster-core` has NO dependencies on other workspace crates
- `weavster-runtime` depends ONLY on `weavster-core`
- `weavster-cli` can depend on any workspace crate
- Keep `weavster-runtime` minimal for small Docker images

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
- Documentation is part of "done" for all features and fixes

## Documentation Site

User-facing documentation lives in `/docs` (Docusaurus) and is published to https://docs.weavster.dev

```bash
cd docs
npm install
npm start           # Dev server at localhost:3000
npm run build       # Production build
```

Docs are automatically deployed via GitHub Actions:
- **Push to main** → Deploys `next` version
- **Release published** → Creates version snapshot and deploys

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
