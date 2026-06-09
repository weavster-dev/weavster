# RFC 0003 — the Weavster engine (Rust + WASM runtime)

- Status: **Accepted** (design resolved; no implementation yet)
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
engine is the production runtime. **Transforms always run as WASM in both** — the only
difference is the harness around them: a TS host drives the wasm locally, the Rust engine
drives the very same wasm in production. There is one transform implementation _and one
execution path_, not two. Because both harnesses run the identical compiled module, there is
no V8-vs-QuickJS divergence to reconcile; the parity test (slice 6) is "same module, two
hosts," not "two engines we hope agree."

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
   that _produces_ WASM lives in the (Rust-buildable) tool path, not in the runtime image.
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

```text
applyFlow (v0alpha2 engine)  +  the flow's _ts functions  +  format-pack parse/serialize
        └──────────────── one JS module ────────────────┘  ──Javy──▶  flows/<name>.wasm
```

So the WASM module is the **whole middle of the pipeline**: parse bytes → `applyFlow` →
serialize bytes. The host never parses YAML, never knows the DSL — it writes a small input
envelope in and reads a result envelope out. The same `applyFlow` that runs under Node for
`weavster test` and `weavster run` runs, byte-for-byte the same logic, inside the engine.

```text
source (host) ──envelope──▶  wasm[ parse → applyFlow → serialize ]  ──envelope──▶ sink (host)
```

The module bundles **all format packs**; the source/sink format is not baked in but passed at
runtime in the envelope (see [the host ABI](#the-wasm-transform-host)). One module per flow,
reusable across pipelines that pick different formats.

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

`manifest.json` is the contract between CLI and engine — it carries **two independent versions**,
both decoupled from the project's `apiVersion`:

- `manifestVersion` — schema version of the manifest file itself. The engine refuses an unknown
  major so the file shape can evolve safely.
- `abiVersion` — which host ABI the wasm modules were built against (the Javy stdin/stdout
  contract). The engine refuses a module it cannot drive, instead of producing garbage when a
  future Javy/wasmtime upgrade shifts the ABI under an old artifact.

```jsonc
{
  "manifestVersion": "1",
  "abiVersion": "javy-1",
  "pipelines": [
    {
      "name": "orders",
      "source": { "type": "file", "glob": "in/*.json", "format": "json" },
      "flow": "order", // → flows/order.wasm
      "sink": { "type": "file", "path": "out/order.json", "format": "json" },
    },
  ],
}
```

Connector config is inline per pipeline (`source`/`sink`); the `flow→wasm` map is the `flow`
field resolved by convention to `flows/<flow>.wasm`. `format` is a runtime field the host copies
into the input envelope — it is _not_ baked into the wasm.

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
  Each pipeline is `source → transform → sink` fed by a **FIFO queue with concurrency 1** —
  one message in flight, processed in order. Pipelines run concurrently _with each other_;
  _within_ a pipeline, strictly serial. (Higher per-pipeline concurrency is a `TODO(config)` —
  the queue is the seam.)
- **Scope errors** per document exactly as RFC 0002 specifies — startup errors abort non-zero;
  for per-document errors, **log and move on**: fail a bounded run, but on a live stream log the
  failing document (pipeline + document + stage) and continue. No checkpoint/resume yet —
  at-least-once durability is a later slice.
- **Observe** — structured logs now; metrics later.

Dev of the engine itself: `cargo run -- -c ./weavster.yaml`. A user never builds Rust — they
author config, run `weavster compile`, and hand the artifact to the prebuilt engine image.

### The WASM transform host

The host contract is Javy's default ABI: write an **input envelope** to the instance's stdin,
run, read a **result envelope** from stdout. One instance processes **exactly one document**
(see [Connectors](#connectors) — file-level fan-out is the connector's job, not the transform's).
Instances are pooled and reused across documents (re-init between calls) to amortize
instantiation. Transforms are **synchronous** (QuickJS, and `_ts` functions, are sync) —
consistent with the existing function model.

**Envelopes.** The format travels with the document, so one module serves every format:

```jsonc
// stdin — input envelope
{ "format": "json", "payload": <bytes> }

// stdout — result envelope (industry-standard result/either shape)
{ "ok": true,  "payload": <bytes> }
{ "ok": false, "error": { "stage":   "parse" | "transform" | "serialize",
                          "type":    "...",        // error class
                          "message": "...",
                          "detail":  { } } }       // extension point for custom handling later
```

The `stage` field is what lets the host attribute a failure to parse / transform / serialize —
information that lives inside the wasm and is otherwise invisible across the byte boundary. The
host maps a `false` envelope onto RFC 0002's error scoping (fail a bounded run; log on a live
stream) and reports pipeline + document + `stage`. Custom error-handling policies are a later
slice; `detail` is the seam they hang off.

```rust
// sketch — host side (`EngineResult<T>` alias defined with the connector traits below)
let out: Bytes = host.run(&flow_wasm, Input { format, payload })?;  // stdin → run → stdout
```

**Resource limits.** The wasm is sandboxed _and_ bounded. Defaults now, configurability later:

| Limit            | Default       | TODO                        |
| ---------------- | ------------- | --------------------------- |
| Memory per inst. | 128 MB        | `// TODO(config)` per-flow  |
| Wall-clock       | 5 s (epoch)   | `// TODO(config)` per-flow  |
| Concurrency      | pooled, 1 doc | `// TODO(config)` pool size |

These ship as constants with `TODO(config)` markers — no config surface in this phase, but the
wasmtime knobs (memory limiter, epoch interruption) are wired so a runaway `_ts` cannot hang or
exhaust the engine.

### Connectors

A connector is a typed `Source` or `Sink`, mirroring RFC 0002's TS interface so the two
runtimes stay aligned:

```rust
type EngineResult<T> = Result<T, EngineError>;

#[async_trait]
trait Source {
    /// Yields one document at a time; `None` at end-of-stream.
    async fn next(&mut self) -> Option<EngineResult<Bytes>>;
}

#[async_trait]
trait Sink {
    async fn write(&mut self, doc: Bytes) -> EngineResult<()>;
}
```

**Each `next()` yields exactly one document — one message per transform invocation.** Anything
"multi" — a glob matching many files, an array or JSONL file holding many records — is the
_connector's_ concern, not the transform's. This keeps the wasm contract dead simple (one doc in,
one result out) while the connector side absorbs the variability, which is where it belongs.

**Paths are globs, not single files**, so the same shape works across backends: a local
directory glob, an SFTP remote glob, a blob-store key prefix all collapse to "enumerate matches,
yield each." The glob resolves against a connector **root** (the artifact mount dir by default).
For this RFC the `file` connector maps **one file → one document**; a file that holds many records
(array/JSONL) is a **`// TODO` expansion** — when it lands, it changes only the connector, never
the transform or the host ABI.

```jsonc
"source": { "type": "file", "glob": "in/*.json", "format": "json" }
```

Connectors are resolved from the manifest through a **registry** keyed by `type`. This RFC
implements **`file` only** — but behind the trait + registry, so the remaining types slot in
later without touching the run loop:

| Type   | Status      | As source / sink                 |
| ------ | ----------- | -------------------------------- |
| `file` | this RFC    | read / write a path              |
| `rest` | later slice | HTTP poll-or-webhook / HTTP call |
| `blob` | later slice | object-store get / put           |
| `tcp`  | later slice | socket read / write              |
| `grpc` | later slice | stream recv / send               |
| `db`   | later slice | query rows / upsert rows         |

Format/schema handling stays where RFC 0002 put it: the **source** format chooses the parser,
the **sink** format chooses the serializer — and both run _inside the WASM module_, not in the
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
ENTRYPOINT ["/engine"]          # reads /etc/weavster/weavster.yaml by default
```

**Invocation.** The engine boots from `weavster.yaml`, the project config, **mounted** into the
container — the nginx/postgres convention: a known default path (`/etc/weavster/weavster.yaml`),
overridable with `-c/--config <path>`. `weavster.yaml` names the artifact location and any
runtime settings; the engine resolves the artifact from there. No bare positional artifact path —
config is the single mounted entrypoint, which is what k8s ConfigMap/volume mounts expect.

```text
/engine                          # default: -c /etc/weavster/weavster.yaml
/engine -c /run/weavster.yaml    # custom location
```

**`weavster.yaml` is the active-pipeline registry.** Pipelines are files — `pipelines/*.yaml`,
the same one-per-file convention as `flows/`. Each pipeline yaml configures _itself_: its source,
transform (flow), and sink. `weavster.yaml` lists which pipelines are **active and their status
(enabled / disabled)** — the project switchboard, not the per-pipeline config:

```yaml
# weavster.yaml
apiVersion: weavster/v0alpha2
name: golden-path
pipelines:
  - name: orders # → pipelines/orders.yaml
    enabled: true
  - name: invoices
    enabled: false # excluded from the compiled artifact
```

`weavster compile` reads `weavster.yaml`, resolves each **enabled** pipeline from its
`pipelines/<name>.yaml`, and emits the manifest containing only those. **Disabled pipelines are
excluded from the manifest entirely** — re-enabling one requires a recompile, which is consistent
with the ahead-of-time artifact model (any change already rebuilds the artifact). The engine runs
exactly what the manifest contains; the operator-facing switch is `enabled` in `weavster.yaml`.

**Single server (now).** One engine process runs all the project's pipelines concurrently.

**Distributed (later).** The _same_ binary runs one pipeline (or a shard) per pod, with
orchestration external. The engine is designed to run one project of N pipelines in-process; the
distributed strategy is N engine instances each scoped to a subset. No engine change is required
to move from one to the other — only how it is invoked. Out of scope to build here; in scope to
not foreclose.

## Slices

These are the design slices. Their milestone breakdown — plus a **slice 0** (stand up the Rust
crate in the currently TS-only monorepo) that precedes slice 1 — lives in
[`docs/ENGINE_PLAN.md`](../ENGINE_PLAN.md) as E0–E6.

1. **Manifest + artifact spec.** Define `manifest.json` (versioned) and the `artifact/` layout.
   The contract first, so CLI and engine can be built against it independently.
2. **CLI `weavster compile`.** Bundle `applyFlow` + format packs + `_ts` per flow → JS → Javy →
   `flows/<name>.wasm`; emit the manifest. (TS-side work; reuses `validate`.)
3. **Engine core.** Rust binary: load manifest, wasmtime host with the Javy stdin/stdout ABI,
   the per-pipeline run loop, and RFC 0002 error scoping. File source/sink only.
4. **Connector trait + registry.** Land the `Source`/`Sink` traits and the `type`-keyed registry
   with `file` as the first (only) entry, so later connectors are additive.
5. **Thin Docker image** + the `cargo run -- -c ./weavster.yaml` dev path, with `weavster.yaml`
   as the mounted, override-able config entrypoint.
6. **Parity test.** A golden pipeline run through both harnesses must produce identical output.
   Since both drive the **same compiled wasm**, this checks the two _hosts_ (TS vs Rust I/O,
   envelope handling, error scoping) agree — not two JS engines. Near-free, and the guardrail
   that keeps the harnesses honest.

## Non-goals (this phase)

- Connectors beyond `file` (REST, blob, TCP, gRPC, DB) — later slices on the same trait.
- Kubernetes / distributed orchestration — design-compatible, not built.
- Secrets management — needed once network/DB connectors arrive (tracks RFC 0002).
- Hot reload of artifacts, multi-tenant isolation, HA, scheduling, autoscaling.
- Retiring the TS `run` loop — it stays as the local dev/test runtime.
- A Rust reimplementation of the v0alpha2 DSL — the DSL only ever lives in `@weavster/core`.

## Resolved (decisions folded into the body)

- **Format ↔ flow binding** → format is a runtime field in the input envelope; the wasm bundles
  all packs; **one module per flow**. (Was: parse/serialize baked into per-flow wasm, which broke
  when two pipelines shared a flow with different formats.)
- **Cross-boundary errors** → a **result envelope** (`ok` / `error{stage,type,message,detail}`)
  carries the failing stage out of the wasm; industry-standard result shape, `detail` is the seam
  for custom handling later.
- **Resource limits** → memory + wall-clock + pooled-single-doc **defaults now**, `TODO(config)`
  for per-flow/pool tuning.
- **One execution path** → transforms **always run as wasm**; local and prod differ only in the
  host harness, so there is no JS-engine divergence and the parity test is near-free.
- **Engine invocation** → boots from `weavster.yaml`, mounted at a default path, `-c/--config` to
  override (nginx/postgres convention).
- **Multi-file / multi-record** → connector concern, not the transform's. One message per
  invocation; **1 file → 1 document** now, multi-record-per-file is a `TODO` connector expansion.
- **Per-document failure** → **log and move on** on a live stream (fail a bounded run). No
  checkpoint/resume; at-least-once durability is a later slice.
- **Concurrency** → each pipeline is `source → transform → sink` behind a **FIFO queue,
  concurrency 1** (in-order, one in flight). Pipelines run concurrently with each other; higher
  per-pipeline concurrency is a `TODO(config)`.
- **`weavster.yaml` ↔ pipelines** → pipelines are `pipelines/*.yaml` (configure source/transform/
  sink); `weavster.yaml` is the active-pipeline registry with `enabled`/`disabled` status. Compile
  emits a manifest of the enabled set.

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
6. **`_ts` bundling** — how a function's own imports/deps are bundled (esbuild step) and what is
   disallowed inside the sandbox.
7. **Durability (deferred, not unknown)** — `log-and-move-on` is the chosen behavior now; the open
   work is _when_ at-least-once / checkpointing arrives, which connector types force it (queues,
   DB), and where offsets live. Tracked for a later slice, not blocking this phase.
