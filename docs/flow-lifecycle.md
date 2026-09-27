# Flow lifecycle

Every flow has a runtime `status`. A new flow starts `undeployed`, and only a `started` flow
accepts messages. You change the status with lifecycle operations. You cannot set `status`
when you create a flow: `POST /api/v1/flows` with a `status` field returns `400`.

The normal path is `undeployed → deployed → started`; a started flow can be paused or halted
(then resumed) and stopped (then started again).

| Operation | From | To |
|---|---|---|
| `deploy` | `undeployed` | `deployed`; every `undeployed` flow it depends on (directly or indirectly) is deployed first |
| `start` | `deployed`, `stopped` | `started` |
| `pause` | `started` | `paused` |
| `halt` | `started` | `halted` |
| `resume` | `paused`, `halted` | `started` |
| `stop` | `started`, `paused`, `halted` | `stopped` |
| `undeploy` | `deployed`, `started`, `paused`, `halted`, `stopped` | `undeployed` |

## Run an operation

These operations need the `flows:deploy` permission (or `admin`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/deploy
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/start
```

Each returns `200` with the updated flow:

```json
{"id":"adt","name":"ADT normalize","sourceType":"http","status":"started","enabled":false}
```

| Response | Cause |
|---|---|
| `409 cannot pause a flow that is stopped` | The operation is not allowed from the flow's current status (see the table). |
| `409 dependency problem: flow top depends on missing flow base` | `deploy` found a dependency that no longer exists. |
| `404` | Unknown flow, or an operation name that is not one of the seven above. |
| `403` | You lack `flows:deploy`. This is checked first, so an unknown operation also returns `403` for such users. |

Every status change is logged as an event of type `flow.<new status>` (for example
`flow.started`), with `data.from` holding the previous status. See `GET /api/v1/events`.

Every operation except `halt` waits for that flow's messages that are being processed right
now, then changes the status. `halt` is a force-stop: it changes the status at once, lets
those messages finish, and accepts or retries nothing new. Operations on one flow never wait
for another flow. The new status is saved and survives a restart.

## Redeploy all flows

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/redeploy-all
```

This undeploys and re-deploys every flow that is not `undeployed`, one flow at a time. Each
of them ends **`deployed`**, so start the ones that should process messages again. The
response lists the redeployed flows. If it fails part-way, it returns
`500 {"error":{"code":"REDEPLOY_INCOMPLETE",…},"redeployed":[…]}`, listing the flows already
redeployed. The rest keep their status.

## Act on all flows at once

`POST /api/v1/flows/{action}-all` runs one action on every flow it applies to (permission
`flows:deploy`). The actions are `deploy`, `undeploy`, `start`, `stop`, `pause`, `halt`, and `resume`:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/start-all
```

```json
{"changed":["adt","orm"],"skipped":[{"id":"legacy","reason":"invalid lifecycle transition: cannot start a flow that is undeployed"}]}
```

- Flows are handled in dependency order: dependencies first for `deploy`, `start`, and `resume`;
  dependents first for the others.
- A flow the action does not apply to (for example `start` on an `undeployed` flow) is listed in
  `skipped` with the reason; the others still change.
- `deploy-all` skips disabled flows (`"reason":"disabled"`), as automatic deployment at startup
  does. An enabled flow's deploy still deploys its undeployed dependencies, disabled or not, as a
  single `deploy` does; such a dependency is then listed in `changed`.
- `changed` lists every flow whose status the call changed.
- If the store fails part-way, the reply is
  `500 {"error":{"code":"TRANSITION_INCOMPLETE",…},"changed":[…],"skipped":[…]}`.

## What each status means for messages

| Status | New messages (`POST /flows/{id}/messages`) | Queued retries |
|---|---|---|
| `started` | processed | retried |
| any other | `409 flow … is <status>; start it first` | kept `queued`, not retried |

## Stopping one destination

You can hold deliveries to a single destination while the rest of the flow keeps running
(permission `flows:deploy`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/destinations/ehr/stop
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/destinations/ehr/start
```

- `stop` waits for messages of the flow that are being processed right now, so nothing reaches
  `ehr` after the stop is confirmed. `start` takes effect at once.
- While `ehr` is stopped, messages are still delivered to the other destinations. They stay
  `queued` for `ehr`, which uses none of their retry attempts. A message is not dead-lettered
  while it is still held for a stopped destination.
- After `start`, the retry worker delivers the held messages (same `Idempotency-Key`).
- The flow shows `"stoppedDestinations": ["ehr"]`. This is runtime state, like `status`: it
  survives a restart and every status change, an update keeps it (a destination the update
  removes is dropped from it), and exports leave it out.
- You cannot set `stoppedDestinations` in a create, update, or import body.
- Stopping or starting logs `flow.destination.stopped` / `flow.destination.started`.
- An unknown flow or destination returns `404`. Stopping a stopped destination (or starting a
  running one) changes nothing.

## Deleting a running flow

`DELETE /api/v1/flows/{id}` works in any status. For a flow that is not `undeployed`, the server
waits for that flow's in-flight messages, sets it to `undeployed` (a `flow.undeployed` event), and
then removes it (a `flow.deleted` event). New messages for it return `404`. Its `queued` messages are
`dead-lettered` by the next retry pass. A flow that other flows depend on cannot be deleted
(`409`).

## Enabled flows start automatically

`enabled` marks a flow as eligible for automatic deployment. It never changes the status by
itself. When the server starts (with `flows.deployOnStartup`, the default; see
[Server configuration](server-config.md#flows)), every flow that is `enabled` **and**
`undeployed` is deployed into its [initial state](#initial-state) (`started` unless you set
another). Flows in any other status keep it, and disabled flows are
left alone.

Because `enabled` decides what runs after a restart, a flow you only `undeploy` comes back
at the next start if it is still enabled. To keep a flow offline across restarts, `undeploy` it
**and** `disable` it.

Set it on create (`"enabled": true`), with an update that includes `enabled`, or with these
endpoints (permission `flows:edit`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/enable
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/disable
```

Each returns `200` with the flow, or `404` for an unknown flow.

### Initial state

`initialState` sets the status an automatically deployed flow gets:

| `initialState` | Status after the server starts |
|---|---|
| `started` (default, or when omitted) | `started`: accepts messages. |
| `paused` | `paused`: rejects messages until you `resume` it. |
| `stopped` | `stopped`: rejects messages until you `start` it. |

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows \
  -d '{"id":"adt","enabled":true,"initialState":"paused"}'
```

`initialState` only applies to automatic deployment at startup. A manual `deploy` still leaves
the flow `deployed`. Any other value returns `400`.

If a flow cannot be auto-deployed (for example, its stored definition is unreadable), the
server logs `auto-deploy failed` with the flow id and starts anyway. You can then fix or delete
that flow over the API.

## Upgrading from a version without the lifecycle

Flows created before the lifecycle existed have no status, or a free-form one. They read as
`undeployed`. On the first start after the upgrade, those that are `enabled` are deployed and
started automatically (they have no `initialState`, so they start) (with the default `flows.deployOnStartup: true`). The others reject
messages until you `deploy` and `start` them. `queued` messages of a flow are retried again once
it is started.

## Not available yet

- CLI commands for these operations.
