# Topology

The topology API returns your flows as a graph: which flows exist and how they connect, and for one
flow, where its messages come from, how they are transformed, and where they go. It is read-only
and needs the `flows:view` permission. The server computes no layout: nodes and edges carry
structure, status, and counts, never positions.

The JSON Schema of both responses is published as
[`agent-docs/schemas/topology.schema.json`](https://github.com/weavster-dev/weavster/blob/main/agent-docs/schemas/topology.schema.json).

## Overview: every flow

```bash
curl -s -u 'admin:PASSWORD' http://127.0.0.1:8080/api/v1/topology
```

```json
{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z",
 "nodes":[
  {"id":"flow:adt","kind":"flow","label":"ADT inbound","status":"started",
   "activity":{"received":2,"sent":1,"errored":0,"queued":1,"lastMessageAt":"2026-09-29T09:59:58Z"}},
  {"id":"flow:billing","kind":"flow","label":"","status":"started",
   "activity":{"received":0,"sent":0,"errored":0,"queued":0}}],
 "edges":[
  {"id":"edge:flow:adt:route:flow:billing","from":"flow:adt","to":"flow:billing","kind":"route",
   "label":"routeMessage('billing')","status":"active"}]}
```

- One `flow` node per flow, with id `flow:<id>`.
- A `route` edge for each flow a flow sends to (a destination of type `flow`), with the traffic
  of those destinations as its `activity` and `status`, and a `dependency` edge for each flow in
  its `dependsOn`.
- With no flows, `nodes` and `edges` are `[]`.

## One flow: source, transform, destinations

`GET /api/v1/topology/flows/{flowId}` takes the flow's id with or without the `flow:` prefix
(`adt` or `flow:adt`):

```bash
curl -s -u 'admin:PASSWORD' http://127.0.0.1:8080/api/v1/topology/flows/flow:adt
```

```json
{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z",
 "flowId":"flow:adt","flowName":"ADT inbound","flowStatus":"started",
 "nodes":[
  {"id":"source:http","kind":"source","label":"http://:9001/adt","status":"started",
   "activity":{"received":2,"sent":0,"errored":0,"queued":0,"lastMessageAt":"2026-09-29T09:59:58Z"},
   "meta":{"connectorType":"http","dataType":"json"}},
  {"id":"transform:dsl:normalize","kind":"transform","label":"normalize","status":"started",
   "activity":{"received":2,"sent":1,"errored":0,"queued":1,"lastMessageAt":"2026-09-29T09:59:58Z"},
   "meta":{"steps":"1"}},
  {"id":"destination:ehr","kind":"destination","label":"ehr","status":"errored",
   "activity":{"received":0,"sent":0,"errored":2,"queued":0},"meta":{"connectorType":"http"}},
  {"id":"destination:archive","kind":"destination","label":"archive","status":"started",
   "activity":{"received":0,"sent":2,"errored":0,"queued":0},"meta":{"connectorType":"file"}},
  {"id":"destination:tobilling","kind":"destination","label":"tobilling","status":"stopped",
   "activity":{"received":0,"sent":0,"errored":0,"queued":0},"meta":{"connectorType":"flow","flow":"billing"}}],
 "edges":[
  {"id":"edge:source:http:path:transform:dsl:normalize","from":"source:http","to":"transform:dsl:normalize",
   "kind":"message-path","status":"active","activity":{"received":2,"sent":0,"errored":0,"queued":0,"lastMessageAt":"2026-09-29T09:59:58Z"}},
  {"id":"edge:transform:dsl:normalize:path:destination:ehr","from":"transform:dsl:normalize","to":"destination:ehr",
   "kind":"message-path","status":"errored","activity":{"received":0,"sent":0,"errored":2,"queued":0}},
  {"id":"edge:transform:dsl:normalize:path:destination:archive","from":"transform:dsl:normalize","to":"destination:archive",
   "kind":"message-path","status":"active","activity":{"received":0,"sent":2,"errored":0,"queued":0}},
  {"id":"edge:transform:dsl:normalize:path:destination:tobilling","from":"transform:dsl:normalize","to":"destination:tobilling",
   "kind":"message-path","status":"idle","activity":{"received":0,"sent":0,"errored":0,"queued":0}},
  {"id":"edge:destination:tobilling:route:flow:billing","from":"destination:tobilling","to":"flow:billing",
   "kind":"route","label":"routeMessage('billing')"}]}
```

| Node | Id | Label | `meta` |
|---|---|---|---|
| Source | `source:<type>` (`file`, `http`, `mllp`, `database`) | Where messages come from, such as `file:/in (*.hl7)`, `https://:9001/adt`, `mllp://:2575`, or `database:postgres`. Never a password or connection string. | `connectorType`; `dataType` (the flow's `inputFormat`, `json` by default) |
| Transform | `transform:dsl:<name>` (`transform` when the transform has no `name`) | The transform's name | `steps`: how many steps it has |
| Destination | `destination:<name>` | The destination's name | `connectorType`; `flow` for a destination of type `flow` |

- A flow without a `source` (messages only arrive through `POST /api/v1/flows/{id}/messages`) has
  no source node, and one without a `transform` has no transform node. The `message-path` edges
  start at the first node there is. The free-text `sourceType` field does not make a source.
- A destination of type `flow` also has a `route` edge to `flow:<target>`, and the target flow is
  in `nodes` too (with only `id`, `kind`, and `label`), so every edge ends at a node.
- An unknown flow returns `404`.

## Status

Every node's `status` is the flow's lifecycle state (`undeployed`, `deployed`, `started`,
`paused`, `halted`, `stopped`), with two exceptions:

- A stopped destination of a started flow is `stopped`.
- A started flow, or one of its destinations, is `errored` when at least half of its deliveries in
  the last 5 minutes failed. For a destination these are its own delivery attempts; for the flow,
  its messages counted `errored` against those `sent`.

An edge's `status` is `active` when messages crossed it in the last 5 minutes, `errored` when at
least half of them failed, and `idle` otherwise: also for a flow that is not started, and for the
edge into a stopped destination.

The last 5 minutes are judged from the [statistics samples](processing-messages.md#statistics-over-time),
counted from the newest sample taken before them, so with the default `stats.sampleIntervalMs`
(one minute) a change shows at once and ends after about 5 minutes. A lifetime reset in the
window is counted correctly.

## Activity

`activity` holds the counts since the flow's statistics were last
[reset](processing-messages.md#5-statistics-and-events): `received`, `sent`, `errored`, `queued`,
and `lastMessageAt` (left out until the first message). Zero counts are always present.

- A flow node and the transform node count the flow's messages.
- A source node counts every message the flow received: from the source, and also any sent with
  `POST /api/v1/flows/{id}/messages`. A destination node counts its own deliveries.
- A `message-path` edge carries the counts of the node it reaches (for the edge from the source,
  the source's).

With `store.dialect: postgres`, the counts are kept across restarts.

## Common pitfalls

- Poll the endpoints to refresh a view; there is no stream. Every response has a new
  `generatedAt`, and node and edge ids stay the same while the flow does, so a client can update
  a drawing in place.
- `errored` is about the recent past: after the failures stop, it goes back to the lifecycle
  status within 5 minutes.
- Resetting a flow's statistics also resets its `activity`.
