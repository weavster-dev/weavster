# Fixture artifact — `golden-path`

A reference **artifact** showing the on-disk layout the engine consumes (see
[`docs/ARTIFACT_SPEC.md`](../../../../docs/ARTIFACT_SPEC.md)):

```text
golden-path/
  manifest.json        # the CLI↔engine contract (validates against spec/schemas/manifest.schema.json)
  flows/
    order.wasm         # Javy-compiled flow module — emitted by `weavster compile` (E2)
```

The `manifest.json` here is hand-written to pin the contract for E1. The `flows/*.wasm`
modules are **not** checked in — they are build output produced ahead of time by
`weavster compile` (milestone E2). `flows/order.wasm` is named by convention from the
manifest's `pipelines[].flow` field (`order` → `flows/order.wasm`).
