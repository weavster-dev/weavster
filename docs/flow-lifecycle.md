# Flow lifecycle

Every flow has a runtime `status`. A new flow starts `undeployed`, and only a `started` flow
accepts messages. You change the status with lifecycle operations. You cannot set `status`
when you create a flow: `POST /api/v1/flows` with a `status` field returns `400`.

The normal path is `undeployed → deployed → started`; a started flow can be paused or halted
(then resumed) and stopped (then started again).

| Operation | From | To |
|---|---|---|
| `deploy` | `undeployed` | `deployed` |
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

An operation waits for messages that are currently being processed to finish. The new status
is saved and survives a restart.

## Redeploy all flows

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/redeploy-all
```

This undeploys and re-deploys every flow that is not `undeployed`. Each of them ends
**`deployed`**, so start the ones that should process messages again. The response lists the
redeployed flows.

## What each status means for messages

| Status | New messages (`POST /flows/{id}/messages`) | Queued retries |
|---|---|---|
| `started` | processed | retried |
| any other | `409 flow … is <status>; start it first` | kept `queued`, not retried |

## Not available yet

- Starting or stopping a single destination.
- Automatic deployment of `enabled` flows when the server starts.
- Flow dependencies (deploy does not deploy other flows).
- CLI commands for these operations.
