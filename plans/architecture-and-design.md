# Weavster Architecture & Design Plan

This document captures the planned architecture, design decisions, and future features for Weavster. These may evolve as implementation progresses.

## Core Concepts

- **Project**: Collection of flows organized in a directory with `weavster.yaml`
- **Flow**: 1 input → N outputs with transforms in between
- **Connector**: Input/Output adapter (Kafka, Postgres, HTTP, File, etc.)
- **Bridge**: Special connector linking flows together
- **Transform**: Data manipulation (rename, filter, compute, etc.)

## Transform Pipeline

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

## Local Development Runtime

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

## Configuration Hierarchy

```
weavster.yaml           # Project config, runtime settings
├── flows/
│   ├── flow_a.yaml     # Individual flow definitions
│   └── flow_b.yaml
├── connectors/
│   └── kafka.yaml      # Reusable connector configs
└── profiles.yaml       # Environment-specific overrides (like dbt)
```

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

## Planned CLI Commands

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
