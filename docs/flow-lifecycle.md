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
| `404` | Unknown flow, or an operation name that is not one of the seven above. |
| `403` | You lack `flows:deploy`. This is checked first, so an unknown operation also returns `403` for such users. |

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

## What each status means for messages

| Status | New messages (`POST /flows/{id}/messages`) | Queued retries |
|---|---|---|
| `started` | processed | retried |
| any other | `409 flow … is <status>; start it first` | kept `queued`, not retried |

## Deleting a running flow

`DELETE /api/v1/flows/{id}` works in any status. For a flow that is not `undeployed`, the server
waits for that flow's in-flight messages, undeploys it (logging a `flow.undeployed` event with
`"reason":"deleted"`), and removes it. New messages for it return `404`. Its `queued` messages are
`dead-lettered` by the next retry pass. A flow that other flows depend on cannot be deleted
(`409`).

## Enabled flows start automatically

`enabled` marks a flow as eligible for automatic deployment. It never changes the status by
itself. When the server starts (with `flows.deployOnStartup`, the default; see
[Server configuration](server-config.md#flows)), every flow that is `enabled` **and**
`undeployed` is deployed and started. Flows in any other status keep it, and disabled flows are
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

If a flow cannot be auto-deployed (for example, its stored definition is unreadable), the
server logs `auto-deploy failed` with the flow id and starts anyway. You can then fix or delete
that flow over the API.

## Upgrading from a version without the lifecycle

Flows created before the lifecycle existed have no status, or a free-form one. They read as
`undeployed`. On the first start after the upgrade, those that are `enabled` are deployed and
started automatically (with the default `flows.deployOnStartup: true`). The others reject
messages until you `deploy` and `start` them. `queued` messages of a flow are retried again once
it is started.

## Not available yet

- Starting or stopping a single destination.
- CLI commands for these operations.
