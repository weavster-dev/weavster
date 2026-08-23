# Read-Only Graph View — Data Contract (Design Note)

**Status:** Design note (Phase 2)
**Scope:** the read-only web UI's **flow topology/connectivity graph** — the confirmed MVP UI surface (see `target-architecture-wasm-iac.md` §10.1).
**Audience:** API/backend implementers + the future UI implementer.

> **Purpose of this note:** lock the data contract so we don't have to re-derive it later. This is a *data* contract — the backend returns pure graph data; all rendering/layout ("Svelte Flow"-style node/edge) is a **client concern**.

---

## 1. Scope & Principles

- **Read-only.** The UI consumes only `GET` endpoints (and optionally a server-sent stream). No mutation endpoints exist for this surface. Git/CI remains the sole path for config changes.
- **Two levels:** an **overview** graph (flows + inter-flow connectivity) and a **flow-internal** graph (source → transforms → destinations) as a drill-down.
- **Connectivity = static wiring; Activity = live traffic** layered on top.
- **No layout in the backend.** Nodes/edges carry *structure and status only*, never x/y coordinates. Layout is computed client-side (and, for MVP, persisted client-side only).
- **Stable ids.** Every node/edge id is stable and derived from the underlying entity id, so the client can diff/patch the graph on refresh without re-laying-out.

---

## 2. Graph Model

### 2.1 Node

```json
{
  "id": "flow:my-flow:source:file-1",
  "kind": "source",
  "label": "file:///incoming/patients",
  "status": "started",
  "activity": {
    "received": 12841,
    "sent": 12790,
    "errored": 6,
    "queued": 45,
    "lastMessageAt": "2026-08-23T16:40:00Z"
  },
  "meta": {
    "connectorType": "file",
    "dataType": "hl7v2"
  }
}
```

| Field | Type | Notes |
|---|---|---|
| `id` | string | Stable, entity-derived. Prefix by kind: `flow:` / `source:` / `transform:` / `destination:`. |
| `kind` | enum | `flow` \| `source` \| `transform` \| `destination` |
| `label` | string | Human-readable. |
| `status` | enum | `undeployed` \| `deployed` \| `started` \| `paused` \| `halted` \| `stopped` \| `errored` |
| `activity` | object | Rolling-window counters (see §4). Optional (absent when no data). |
| `meta` | object | Type-specific metadata; `connectorType` for source/destination, `dataType`, etc. Optional. |

### 2.2 Edge

```json
{
  "id": "edge:flow:a:route:flow:b",
  "from": "flow:a",
  "to": "flow:b",
  "kind": "route",
  "label": "routeMessage('b')",
  "status": "active",
  "activity": { "sent": 321, "errored": 2 }
}
```

| Field | Type | Notes |
|---|---|---|
| `id` | string | Stable. |
| `from` / `to` | string | Node ids. |
| `kind` | enum | `route` (flow→flow) \| `message-path` (source→transform→destination) \| `dependency` (deployment dependency) |
| `label` | string | Filter/transform name or route condition. Optional. |
| `status` | enum | `active` \| `idle` \| `errored`. Optional. |
| `activity` | object | Rolling counts over the edge. Optional. |

---

## 3. API Contract

All endpoints are read-only, under the existing `/api` root, versioned, JSON. Errors use the platform's standard error envelope.

### 3.1 Overview graph

`GET /api/v1/topology`

Returns all flows as nodes, with `route`/`dependency` edges between them (no flow-internal nodes).

```json
{
  "schemaVersion": "1",
  "generatedAt": "2026-08-23T16:40:00Z",
  "nodes": [ { "kind": "flow", "id": "flow:a", "label": "Patient Admit", "status": "started", "activity": { } } ],
  "edges": [ { "id": "edge:flow:a:route:flow:b", "from": "flow:a", "to": "flow:b", "kind": "route" } ]
}
```

### 3.2 Flow-internal graph (drill-down)

`GET /api/v1/topology/flows/{flowId}`

Returns that flow's source, transform stages, and destinations as nodes, with `message-path` edges, plus any outbound `route` edges to other flows.

```json
{
  "schemaVersion": "1",
  "generatedAt": "2026-08-23T16:40:00Z",
  "flowId": "flow:a",
  "flowName": "Patient Admit",
  "flowStatus": "started",
  "nodes": [
    { "id": "source:file-1", "kind": "source", "label": "file:///incoming/patients", "status": "started", "meta": { "connectorType": "file" } },
    { "id": "transform:yaml:normalize-patient-name", "kind": "transform", "label": "normalize-patient-name", "status": "started" },
    { "id": "destination:tcp-mllp-1", "kind": "destination", "label": "HIS MLLP", "status": "started", "meta": { "connectorType": "tcp", "transmissionMode": "mllp" } }
  ],
  "edges": [
    { "id": "edge:source:file-1:path:transform:yaml:normalize-patient-name", "from": "source:file-1", "to": "transform:yaml:normalize-patient-name", "kind": "message-path", "status": "active" }
  ]
}
```

### 3.3 Activity (deep-dive) — deferred

The rolling `activity` snapshot embedded above is the MVP signal. Deep historical/trend data stays in Prometheus (`metrics_exporter` port) and is **not** re-exposed here. A dedicated time-series/topology analytics endpoint is an Enterprise item (see open questions).

---

## 4. Activity Semantics & Data Source

- `activity` is a **rolling snapshot** (configurable window, default e.g. 5 min / 1 h) computed from the scheduler/executor's in-memory counters and flushed to `Store` periodically. It is **best-effort** and does not require a running Prometheus for the UI to work.
- Counters: `received`, `sent`, `errored`, `queued`, `lastMessageAt`. Node `activity` aggregates its own traffic; edge `activity` is the traffic flowing *across* that edge.
- `status` is derived from the authoritative flow/connector lifecycle state (functional spec §6.1), with `errored` synthesized when a node's error rate exceeds a threshold in the window (or from an explicit error condition).
- **Refresh model (recommended):** simple client polling of the JSON endpoints for MVP (KISS, read-only, cacheable). **Optional later:** `GET /api/v1/topology/stream` as Server-Sent Events for live `activity` patches. No WebSockets.

---

## 5. Authorization

- The graph view requires the **flow-view** permission (the `flows:view`-equivalent from the functional spec permission set). Read-only, so no other grants.
- Same anti-CSRF/TLS/auth middleware as the rest of the API. Node/edge content must not leak cross-tenant data (multi-tenancy scoping ties to the Enterprise multi-tenancy extension point).

---

## 6. Non-Goals (read-only surface)

- No deploy / undeploy / start / stop / pause / resume / edit / rename / remove from this view.
- No node-position persistence server-side (client-side layout; optional client-localStorage persistence is a UI-only concern).
- No streaming of message *content* — the graph shows counters/status, not payloads (message inspection is a separate, deferred read-only view).

---

## 7. Open Questions / Deferred Decisions

| # | Question | Current lean |
|---|---|---|
| 1 | Poll vs SSE for live activity | Poll for MVP; SSE optional later. |
| 2 | Layout persistence for shared dashboards | Client-only (localStorage) for MVP; server-stored shared layouts = Enterprise. |
| 3 | Should `activity` be server-retained (small ring buffer) or purely derived from Prometheus? | Server rolling snapshot for MVP (UI works without Prometheus); Prometheus for deep-dive. |
| 4 | Multi-tenant scoping of `topology` ids | Follow the Enterprise multi-tenancy extension point; single-tenant for MVP. |

---

*Cross-referenced from `target-architecture-wasm-iac.md` §10.1. This note is a Phase 2 design artifact; the endpoints/schema are **proposed** and subject to the JSON-Schema publication step in `agent-docs/schemas/` during implementation.*
