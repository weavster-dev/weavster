# Engine Plan — production runtime (RFC 0003)

The MVP ([archive](./archive/MVP_PLAN.md)) proved config-first authoring, local validation,
fixture testing, the v0alpha2 DSL, and `weavster run` ([RFC 0002](./rfcs/0002-run-pipelines.md)).
This plan covers the next phase: **make it run in production** — the thin Rust + WASM engine
designed in [RFC 0003](./rfcs/0003-engine-runtime.md).

Read RFC 0003 first; this file is the milestone breakdown of its slices, not a re-statement of
the design. The working rules, code-understanding checklist, and definition of done from the MVP
still apply — see [`archive/MVP_TASKS.md`](./archive/MVP_TASKS.md).

## Phase thesis

One portable **artifact** (built ahead of time by `weavster compile`) runs in a thin Rust engine.
Transforms **always** run as WASM — the local TS `run` loop and the prod Rust engine are two
_hosts_ around the same compiled module, so there is one execution path, not two. The engine owns
I/O and orchestration; it never sees the DSL.

Locked decisions (from the RFC 0003 review — see its _Resolved_ section):

- **Always-WASM.** Local and prod differ only in the host harness.
- **One module per flow.** Bundles all format packs; the source (`in`) and sink (`out`) formats
  ride in the input envelope, so one module parses the source and serializes the sink (conversions
  included).
- **Result envelope.** `ok` / `error{stage,type,message,detail}` — `stage` surfaces parse vs
  transform vs serialize across the wasm boundary. Industry-standard result shape.
- **FIFO, concurrency 1.** Each pipeline is `source → transform → sink` behind an in-order queue,
  one message in flight. Pipelines run concurrently with each other.
- **Log-and-move-on.** Per-document failures log and continue on a live stream (fail a bounded
  run). No checkpoint/resume yet.
- **`weavster.yaml` is the switchboard.** Lists active pipelines + `enabled`/`disabled`;
  `pipelines/*.yaml` configure each source/transform/sink. Engine boots from a mounted
  `weavster.yaml`, `-c/--config` to override.

## Non-goals (this phase)

Carried from RFC 0003: connectors beyond `file`; Kubernetes/distributed orchestration; secrets
management; hot reload; multi-tenant isolation, HA, scheduling, autoscaling; retiring the TS `run`
loop; at-least-once durability / checkpointing; any Rust reimplementation of the DSL.

## Spikes (de-risk before the milestone that depends on them)

These are RFC 0003's open questions. Time-box each; the answer feeds the milestone in brackets.

- **S1 — QuickJS-safe bundle.** Can `applyFlow` + the JSON pack + the XML pack (fast-xml-parser)
  bundle to JS with no `node:` builtins and no async? What fails; polyfill vs forbid. **[E2]**
- **S2 — Javy linking.** Static per-flow modules vs a shared QuickJS provider. Default static;
  measure artifact size. **[E1/E2]**
- **S3 — `_ts` bundling.** How a function's own imports/deps bundle (esbuild) and what is
  disallowed in the sandbox. **[E2]**
- **S4 — instance lifecycle.** Re-init a pooled instance vs fresh instantiate per document;
  Wizer preinit as a later optimization. **[E3]**
- **S5 — large/streaming documents.** Javy's stdin/stdout is whole-buffer per call. **[E3, may
  defer]**
- **S6 — artifact shape.** Directory vs tarball/OCI for distribution. **[E1]**

---

## E0 — Engine workspace

Stand up the Rust side of the (currently TS-only) monorepo without disturbing it.

- [ ] Add an `engine/` Rust crate (binary). → verify: `cargo build` from a clean checkout.
- [ ] Wire it into CI as a separate job (build + clippy + test); reuse the dormant
      `rust-coverage` job already stubbed in `codecov.yml`. → verify: CI green on a no-op PR.
- [ ] Settle workspace layout: `engine/` top-level alongside the pnpm packages; document the
      build boundary (TS toolchain never enters the engine image). → verify: README + CONTRIBUTING
      describe where Rust lives.

## E1 — Manifest + artifact spec (the contract)

Define the contract first so CLI (E2) and engine (E3) can be built against it independently.
(RFC 0003 slice 1.)

- [ ] Specify `manifest.json`: `manifestVersion`, `abiVersion` (Javy ABI pin), and the per-pipeline
      `{name, source, flow, sink}` shape with inline connector config. → verify: a hand-written
      golden manifest validates against a published JSON schema.
- [ ] Specify the `artifact/` layout (`manifest.json` + `flows/<name>.wasm`). Decide directory vs
      tarball (**S6**). → verify: the layout is documented and a fixture artifact exists.
- [ ] Define the **input/result envelope** byte contract (`in`/`out` formats + payload in;
      `ok`/`error{stage}` out) as a shared spec doc both hosts cite. → verify: spec doc lands;
      referenced by E2 and E3.

## E2 — CLI `weavster compile` (TS side)

Bundle each enabled flow → JS → Javy → `flows/<name>.wasm`; emit the manifest. Reuses `validate`.
(RFC 0003 slice 2. Depends on **S1, S3**.)

- [ ] Read `weavster.yaml` (active pipelines + `enabled`), resolve each enabled `pipelines/*.yaml`,
      reuse `validate`. → verify: disabled pipelines are excluded from the manifest.
- [ ] Bundle per flow: `applyFlow` + all format packs + the flow's `_ts` → one QuickJS-safe JS
      module. → verify: bundle passes a `node:`/async guard (the **S1** check).
- [ ] Run Javy to produce `flows/<name>.wasm`; emit `manifest.json` with `manifestVersion` +
      `abiVersion`. → verify: `weavster compile` on `examples/golden-path` yields a runnable
      artifact.
- [ ] Wrap the input/result envelope around the bundle (`in`/`out` format select on stdin;
      `ok`/`error` out). → verify: a JSON→XML pipeline round-trips, and feeding a bad document
      yields an `error{stage:"parse"}` envelope, not a crash.

## E3 — Engine core (Rust)

The thin binary: load manifest, host the WASM, run the loop. File source/sink only.
(RFC 0003 slice 3. Depends on **S4**; **S5** may defer.)

- [ ] Load + validate the manifest; refuse unknown `manifestVersion`/`abiVersion` loudly.
      → verify: a mismatched `abiVersion` fails fast with a clear message.
- [ ] wasmtime host over the Javy stdin/stdout ABI; pool instances, re-init between documents
      (**S4**). → verify: a pooled instance processes N documents with stable output.
- [ ] Per-pipeline run loop: `source → transform → sink` behind a **FIFO queue, concurrency 1**;
      pipelines concurrent with each other. → verify: documents come out in input order.
- [ ] Error scoping: startup errors abort non-zero; per-document errors **log-and-move-on** on a
      live stream, fail a bounded run; report pipeline + document + `stage`. → verify: a poison
      document is logged and the stream continues.
- [ ] Resource limits with `TODO(config)` defaults: memory cap, wall-clock (epoch), pooled
      single-doc. → verify: a runaway `_ts` (infinite loop) is interrupted, not hung.
- [ ] Structured logs. → verify: a run emits pipeline/document/stage fields.

## E4 — Connector trait + registry

Land `Source`/`Sink` behind a `type`-keyed registry with `file` as the only entry, so later
connectors (rest/blob/tcp/grpc/db) are additive. (RFC 0003 slice 4.)

- [ ] `Source::next()` / `Sink::write()` async traits; `type`-keyed registry. → verify: an unknown
      `type` in the manifest fails with a clear error.
- [ ] `file` connector: **glob** source resolved against a connector root; one match → one
      document (1 file → 1 document; multi-record is a `// TODO` expansion). → verify: a glob
      matching three files yields three documents.
- [ ] `file` sink: write a path. → verify: golden output matches byte-for-byte.

## E5 — Thin image + invocation

Ship the engine as a thin Docker image and define how it boots. (RFC 0003 slice 5.)

- [ ] Multi-stage Dockerfile: build with Rust, ship a static binary on distroless/scratch — no
      Node, no TS toolchain. → verify: image builds; `docker run` executes the golden artifact.
- [ ] Boot from a **mounted `weavster.yaml`** at a default path, `-c/--config` override
      (nginx/postgres convention). Resolve where the artifact lives from it. → verify:
      `cargo run -- -c ./weavster.yaml` and the container both run the golden pipeline.

## E6 — Parity test (the guardrail)

A golden pipeline through both harnesses must produce identical output. Because both drive the
**same wasm**, this checks the two _hosts_ agree — not two JS engines. (RFC 0003 slice 6.)

- [ ] Run `examples/golden-path` through the TS `run` loop and the Rust engine; assert byte-equal
      output. → verify: a CI job runs both and diffs.
- [ ] Wire it into CI as a merge gate for engine changes. → verify: a deliberate host divergence
      makes the job fail.

---

## Definition of done (per milestone)

Unchanged from the MVP: tests cover the acceptance criteria, docs reflect actual state, the diff
is reviewed file-by-file, the commit message is clear, and the change can be explained in two or
three sentences. New code reaches the project's coverage bar. See
[`archive/MVP_TASKS.md`](./archive/MVP_TASKS.md) for the full checklist.

## Sequencing

E0 → E1 (contract) → E2 ∥ E3 (built against the contract in parallel) → E4 → E5 → E6. Run **S1**
before E2 and **S4** before E3; both are cheap to de-risk and expensive to discover late.
