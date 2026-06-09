# Artifact spec — the CLI ↔ engine contract (Engine Plan E1)

This is the contract both hosts cite: the CLI (`weavster compile`, **E2**) _produces_ the
artifact, the engine (**E3**) _consumes_ it. Defining it first lets E2 and E3 be built
independently. It implements [RFC 0003](./rfcs/0003-engine-runtime.md) slice 1; read the RFC for
the design rationale, this doc for the concrete shapes.

There are three pieces: the **artifact layout**, the **manifest** (`manifest.json`), and the
**WASM envelope** byte contract.

## Artifact layout

An artifact is a **directory** (decision **S6**, below):

```text
artifact/
  manifest.json          # the contract — versioned; pipelines + connector config + flow→wasm map
  flows/
    order.wasm           # Javy-compiled flow module, one per flow
    invoice.wasm
```

- `manifest.json` is the single entry point; the engine reads it and nothing else from the
  user's project (it never sees the DSL or YAML).
- `flows/<flow>.wasm` is resolved **by convention** from each pipeline's `flow` field — `flow:
"order"` → `flows/order.wasm`. One module per flow, reused across pipelines that pick different
  formats (the module bundles all format packs).
- A fixture artifact lives at
  [`spec/examples/artifact/golden-path/`](../spec/examples/artifact/golden-path/) (the `.wasm`
  modules are build output and are not checked in).

### S6 — directory vs tarball

**Decision: a plain directory this phase.** It is what a k8s volume/ConfigMap mount and a local
`cargo run` both expect with zero unpacking, and it keeps the engine free of an archive
dependency. A single-file tarball / OCI artifact for distribution is a **later** concern — it
wraps this layout without changing it, so deferring costs nothing. (RFC 0003 open question 5.)

## Manifest (`manifest.json`)

Schema: [`spec/schemas/manifest.schema.json`](../spec/schemas/manifest.schema.json). Golden +
invalid fixtures: [`spec/examples/manifest/`](../spec/examples/manifest/).

```jsonc
{
  "manifestVersion": "1", // schema version of this file; engine refuses an unknown value
  "abiVersion": "javy-1", // host ABI the wasm was built against; engine refuses a module it can't drive
  "pipelines": [
    {
      "name": "orders", // matches pipelines/<name>.yaml
      "source": { "type": "file", "glob": "in/*.json", "format": "json" },
      "flow": "order", // → flows/order.wasm
      "sink": { "type": "file", "path": "out/order.json", "format": "json" },
    },
  ],
}
```

- **Two independent versions, both decoupled from the project `apiVersion`.**
  `manifestVersion` guards the file shape; `abiVersion` guards the wasm host contract. The engine
  refuses an unknown value for either, loudly, rather than producing garbage (E3).
- **Only enabled pipelines appear.** `weavster.yaml` is the switchboard (`enabled`/`disabled`);
  compile emits a manifest of the enabled set only. Re-enabling a pipeline requires a recompile.
- **Connector config is inline** per pipeline (`source`/`sink`). `file` is the only connector
  this phase; the registry of `type`s grows additively (E4). `glob` (source) and `path` (sink)
  resolve against the connector root (the artifact mount dir by default).
- **`format` is a runtime value, not baked into the wasm.** The source `format` selects the
  parser and the sink `format` selects the serializer; the host copies both into the input
  envelope (below), so one module serves every format and every conversion (e.g. JSON→XML).

## WASM envelope (the host ABI)

The host writes an **input envelope** to the instance's stdin, runs, and reads a **result
envelope** from stdout — Javy's default whole-buffer stdin/stdout ABI, pinned by `abiVersion`.
One instance processes **exactly one document**; file-level fan-out is the connector's job, not
the transform's. Transforms are **synchronous**.

```jsonc
// stdin — input envelope
{ "in": "json", "out": "xml", "payload": <document bytes> }
//   in:      source format → selects the parser
//   out:     sink format   → selects the serializer (may differ from `in` → conversion)
//   payload: the raw source document
```

```jsonc
// stdout — result envelope (result/either shape)
{ "ok": true,  "payload": <serialized bytes> }

{ "ok": false, "error": {
    "stage":   "parse" | "transform" | "serialize", // where it failed, across the byte boundary
    "type":    "...",      // error class
    "message": "...",      // human-readable
    "detail":  { }         // extension point for custom handling (later)
} }
```

- `in`/`out` are the manifest `format` values; `payload` is the document the connector yielded.
- **`stage`** is the field that lets the host attribute a failure to parse vs transform vs
  serialize — information that otherwise dies inside the wasm. The host maps a `false` envelope
  onto RFC 0002 error scoping: fail a bounded run; **log-and-move-on** on a live stream; report
  pipeline + document + `stage`.
- `detail` is the seam custom error-handling policies hang off later; unused this phase.

## Who cites this

- **E2** (`weavster compile`, TS): emits an artifact in this layout — wraps each flow bundle in
  the envelope contract and writes a manifest that validates against the schema.
- **E3** (engine, Rust): loads + validates the manifest, refuses unknown versions, and drives the
  wasm over this envelope ABI.

The [parity test (E6)](./ENGINE_PLAN.md) is what keeps both hosts honest against this one
contract.
