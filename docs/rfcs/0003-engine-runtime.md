# RFC 0003 — the Weavster engine (Rust + WASM runtime)

- Status: **Draft** (design only; no implementation yet)
- Phase: "make it run in production" (the deployable runtime)
- Builds on: `weavster run` and pipelines ([RFC 0002](./0002-run-pipelines.md)) and the
  v0alpha2 DSL ([RFC 0001](./0001-v0alpha2-dsl.md))
- Defers: this is the "Rust/WASM production runtime" RFC 0002 listed as a non-goal

## Summary

A **thin Rust engine** runs a compiled Weavster project in production. It loads a portable
**artifact** (produced ahead of time by the CLI), drives **connectors** (sources and sinks)
for I/O, and runs each flow as a **WASM module** — TypeScript transpiled to JS and embedded
in a QuickJS interpreter compiled to WASM (Javy). The engine ships as a thin Docker image
and is the single deployable for both the local single-server strategy now and a distributed
(Kubernetes) strategy later.

The existing TS `run` loop (RFC 0002) stays — it is the local dev/test runtime. The Rust
engine is the production runtime. **Both honor the same config contract and reuse the same
`@weavster/core` transform code** (Node runs it directly; the engine runs the very same code
compiled into WASM). There is one transform implementation, not two.

## Motivation

`weavster run` (RFC 0002) moves real data, but it runs under Node from a project directory —
fine for a developer's machine, wrong for production. Production wants a single, dependency-free
artifact, a thin image, predictable resource use, and a sandbox around user code. None of that
is Node's strength.

The constraints the user set:

1. **Ship as a thin Docker image** that works both for a local single server now and a future
   Kubernetes distributed strategy. Same binary; only the deployment topology changes.
2. **Transforms are TypeScript compiled to WASM.** User code runs sandboxed, deterministically,
   and identically in dev and prod.
3. **The engine is Rust** — it owns I/O, orchestration, and the WASM host, and the toolchain
   that *produces* WASM lives in the (Rust-buildable) tool path, not in the runtime image.
4. **A small, fixed set of connection types** — file, REST, blob, TCP/IP, gRPC, and DB
   read/write — behind one pluggable interface, with format/schema handling that varies per
   connector.

## How "TypeScript compiles to WASM" actually works

There is no direct TS→WASM compiler. The realistic, mature path is to **embed a JS engine**:
transpile TS→JS, then compile that JS together with a QuickJS interpreter into a single WASM
module ([Javy](https://github.com/bytecodealliance/javy)). The result is real TS/JS semantics
in a sandbox, driven over a simple stdin→stdout byte ABI.

Crucially, the thing we compile is **not a Rust reimplementation of the DSL** — it is the
existing `@weavster/core` code. A flow is compiled by bundling, per flow:

```
applyFlow (v0alpha2 engine)  +  the flow's _ts functions  +  format-pack parse/serialize
        └──────────────── one JS module ────────────────┘  ──Javy──▶  flows/<name>.wasm
```

So the WASM module is the **whole middle of the pipeline**: parse bytes → `applyFlow` →
serialize bytes. The Rust engine never parses YAML, never knows the DSL — it feeds raw bytes
in and writes raw bytes out. The same `applyFlow` that runs under Node for `weavster test` and
`weavster run` runs, byte-for-byte the same logic, inside the engine.

```text
source (Rust) ──bytes──▶  wasm[ parse → applyFlow → serialize ]  ──bytes──▶ sink (Rust)
```

## Ahead-of-time compile and the artifact

Compilation happens **ahead of time in the CLI** (`weavster compile`), not in the engine. This
is what keeps the runtime image thin: the engine carries no TS toolchain, no Node, no bundler —
only a Rust binary and the WASM it runs.

`weavster compile` resolves and validates the project (reusing `validate`), bundles each flow as
above, runs Javy, and emits a portable **artifact**:

```text
artifact/
  manifest.json          # versioned; pipelines, connector config, flow→wasm map
  flows/
    order.wasm           # Javy-compiled flow module
    invoice.wasm
```

`manifest.json` is the contract between CLI and engine — it has its **own version**, decoupled
from the project's `apiVersion`, so the DSL schema can churn without breaking the engine:

```jsonc
{
  "manifestVersion": "1",
  "pipelines": [
    {
      "name": "orders",
      "source": { "type": "file", "path": "in/order.json", "format": "json" },
      "flow": "order",                     // → flows/order.wasm
      "sink":   { "type": "file", "path": "out/order.json", "format": "json" }
    }
  ]
}
```

The engine consumes the artifact; it does not read the user's project directory. The CLI owns
schema, validation, and bundling; the engine owns execution. The CLI boundary from the MVP plan
holds — the engine just sits on the production side of it.

## The engine

A thin Rust binary. Responsibilities, and nothing more:

- **Load** the artifact manifest.
- **Instantiate** flow WASM modules with [wasmtime](https://wasmtime.dev/); pool instances.
- **Drive connectors** — async source/sink I/O.
- **Run the loop**, one per pipeline, reusing RFC 0002's continuous semantics: a bounded source
  (file) yields one document then closes; an unbounded source streams until end-of-stream.
- **Scope errors** per document exactly as RFC 0002 specifies — startup errors abort non-zero;
  per-document errors fail a bounded run but only log on a live stream; report pipeline +
  document + stage.
- **Observe** — structured logs now; metrics later.

Dev of the engine itself: `cargo run -- ./artifact`. A user never builds Rust — they author
config, run `weavster compile`, and hand the artifact to the prebuilt engine image.

### The WASM transform host

The host contract is Javy's default ABI: write the input document to the instance's stdin, run,
read the output document from stdout. One instance processes one document; instances are pooled
and reused across documents (re-init between calls) to amortize instantiation. Transforms are
**synchronous** (QuickJS, and `_ts` functions, are sync) — consistent with the existing
function model.

```rust
// sketch — host side
let out: Bytes = host.run(&flow_wasm, input_bytes)?;   // stdin → run → stdout
```

### Connectors

A connector is a typed `Source` or `Sink`, mirroring RFC 0002's TS interface so the two
runtimes stay aligned:

```rust
#[async_trait]
trait Source {
    /// Yields once for a file; many times for a stream; `None` at end-of-stream.
    async fn next(&mut self) -> Option<Result<Bytes>>;
}

#[async_trait]
trait Sink {
    async fn write(&mut self, doc: Bytes) -> Result<()>;
}
```

Connectors are resolved from the manifest through a **registry** keyed by `type`. This RFC
implements **`file` only** — but behind the trait + registry, so the remaining types slot in
later without touching the run loop:

| Type    | Status      | As source / sink                          |
| ------- | ----------- | ----------------------------------------- |
| `file`  | this RFC    | read / write a path                       |
| `rest`  | later slice | HTTP poll-or-webhook / HTTP call          |
| `blob`  | later slice | object-store get / put                    |
| `tcp`   | later slice | socket read / write                       |
| `grpc`  | later slice | stream recv / send                        |
| `db`    | later slice | query rows / upsert rows                  |

Format/schema handling stays where RFC 0002 put it: the **source** format chooses the parser,
the **sink** format chooses the serializer — and both run *inside the WASM module*, not in the
connector. A connector only moves bytes, so a new connector type never re-implements a format.

## Deployment

**Thin image, multi-stage.** Build the engine with Rust; ship a static binary plus the wasmtime
runtime on a minimal base (distroless/scratch). The artifact is mounted or baked in. No Node, no
TS toolchain in the image — that all ran at compile time in the CLI.

```dockerfile
# sketch
FROM rust:slim AS build
# ... cargo build --release
FROM gcr.io/distroless/cc
COPY --from=build /engine /engine
ENTRYPOINT ["/engine"]          # run: /engine /artifact
```

**Single server (now).** One engine process runs all the project's pipelines concurrently.

**Distributed (later).** The *same* binary runs one pipeline (or a shard) per pod, with
orchestration external. The engine is designed to run one project of N pipelines in-process; the
distributed strategy is N engine instances each scoped to a subset. No engine change is required
to move from one to the other — only how it is invoked. Out of scope to build here; in scope to
not foreclose.

## Slices

1. **Manifest + artifact spec.** Define `manifest.json` (versioned) and the `artifact/` layout.
   The contract first, so CLI and engine can be built against it independently.
2. **CLI `weavster compile`.** Bundle `applyFlow` + format packs + `_ts` per flow → JS → Javy →
   `flows/<name>.wasm`; emit the manifest. (TS-side work; reuses `validate`.)
3. **Engine core.** Rust binary: load manifest, wasmtime host with the Javy stdin/stdout ABI,
   the per-pipeline run loop, and RFC 0002 error scoping. File source/sink only.
4. **Connector trait + registry.** Land the `Source`/`Sink` traits and the `type`-keyed registry
   with `file` as the first (only) entry, so later connectors are additive.
5. **Thin Docker image** + the `cargo run -- ./artifact` dev path.
6. **Parity test.** A golden pipeline run through both the TS `run` loop and the engine must
   produce identical output — the guardrail that keeps the two runtimes honest.

## Non-goals (this phase)

- Connectors beyond `file` (REST, blob, TCP, gRPC, DB) — later slices on the same trait.
- Kubernetes / distributed orchestration — design-compatible, not built.
- Secrets management — needed once network/DB connectors arrive (tracks RFC 0002).
- Hot reload of artifacts, multi-tenant isolation, HA, scheduling, autoscaling.
- Retiring the TS `run` loop — it stays as the local dev/test runtime.
- A Rust reimplementation of the v0alpha2 DSL — the DSL only ever lives in `@weavster/core`.

## Open questions

1. **Javy linking** — self-contained per-flow modules (static QuickJS, simpler, larger) vs a
   shared QuickJS provider with small dynamically-linked flow modules (smaller artifact, more
   moving parts). Proposed: **static first**, revisit if artifact size bites.
2. **QuickJS compatibility of the bundle** — `applyFlow`, the JSON pack, and especially the XML
   pack (fast-xml-parser) must bundle to JS with **no `node:` builtins and no async**. The
   compile step needs a check (or an allowlist) that the bundle is QuickJS-safe; what fails, and
   what do we polyfill vs forbid?
3. **Instance lifecycle** — re-init a pooled instance between documents vs fresh instantiate per
   document (correctness vs throughput). Preinitialization (Wizer) as a later optimization?
4. **Large / streaming documents** — Javy's stdin/stdout is whole-buffer per call; how do we
   handle documents that don't fit a single buffer, or genuinely streaming formats?
5. **Artifact shape** — a directory (above) vs a single tarball/OCI artifact for distribution.
6. **Engine invocation** — which pipelines to run, env/config, and where the artifact comes from
   (mounted path, baked layer, pulled). CLI flags vs env vs a small engine config file.
7. **`_ts` bundling** — how a function's own imports/deps are bundled (esbuild step) and what is
   disallowed inside the sandbox.
