# Processing messages

A flow can transform each message it receives with the YAML DSL and deliver the result to
one or more destinations. You send messages to the flow over the API.

## 1. Create a flow

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows -d '{
  "id": "adt",
  "name": "ADT normalize",
  "sourceType": "http",
  "transform": {
    "kind": "Transform", "name": "normalize", "inputs": ["message"],
    "steps": [
      {"map":    {"from": "PID.5.1", "to": "patient.lastName", "type": "string"}},
      {"filter": {"when": "patient.lastName == '"''"'", "action": "reject"}},
      {"set":    {"field": "patient.label", "expr": "Patient {{patient.lastName}}"}}
    ]
  },
  "destinations": [
    {"name": "ehr",     "type": "http", "url": "https://ehr.example.com/inbound"},
    {"name": "archive", "type": "file", "dir": "/var/lib/weavster/archive"}
  ]
}'
```

The server checks the transform and destinations when you create the flow. A problem is
reported as `400` with the reason, for example:

```text
invalid flow: dsl: normalize: step 1: build: dsl: step not supported yet
```

### `transform`

`transform` is optional. Without it (or with `null` or no `steps`), messages pass through
unchanged and may be any bytes. It uses the steps
below, in order, on the message as a JSON object.

| Step | Fields | Effect |
|---|---|---|
| `map` | `from`, `to`, optional `type` (`string`, `number`, `boolean`) | Copies the value at `from` to `to`, converting it if `type` is set. A missing `from` leaves `to` unchanged. |
| `set` | `field`, `expr` | Sets `field` to `expr`, replacing each `{{path}}` with that value (an empty string when missing). |
| `filter` | `when`, `action` (`reject` or `accept`) | `reject` drops the message when `when` is true; `accept` drops it when `when` is false. |

- **Paths** use dots: `patient.lastName`. A number indexes an array: `items.0.code`.
- **`when`** is either a path (true when the value is present and not empty, `0`, `false`, or `null`),
  or `<operand> == <operand>` / `<operand> != <operand>`. An operand is a path, a quoted string
  (`'x'` or `"x"`), a number (`3`, `-1.5`), or `true`/`false`. A missing path compares equal
  to `''`. No other operators exist.
- `build` and `destinationSet` steps are not supported yet.
- Numbers keep their exact digits (for example 20-digit identifiers) unless a step converts them.

### `destinations`

| Field | Meaning |
|---|---|
| `name` | Unique within the flow; used to report delivery results. |
| `type` | `http` (POST to `url`) or `file` (write one file per message into `dir`, named by message ID). |
| `url` | Required for `http`: an absolute `http://` or `https://` URL. Each delivery is a `POST` with `Content-Type: application/json` (transformed messages) or `application/octet-stream` (passthrough), and it times out after 30 seconds. The request carries an `Idempotency-Key` header, the same value for every attempt to deliver this message to this destination, so the receiver can ignore duplicates. |
| `dir` | Required for `file`. Created if missing. |

## 2. Send a message

You need the `messages:send` permission (or `admin`).

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  http://127.0.0.1:8080/api/v1/flows/adt/messages -d '{"PID":{"5":{"1":"Doe"}}}'
```

```json
{"id":"6f1c…","status":"sent"}
```

The request returns after processing finishes. `status` is one of:

| Status | Meaning |
|---|---|
| `sent` | Delivered to every destination. |
| `queued` | At least one destination failed. The failure is recorded; the message is **not retried yet**. |
| `filtered` | A `filter` step dropped the message. Nothing was delivered. |
| `errored` | The transform failed (for example `"x" is not a number`). Nothing was delivered. |

Processing continues even if your client disconnects, so every message ends in one of the
statuses above.

Errors:

| Response | Cause |
|---|---|
| `400` | The flow has a transform and the body is not a JSON object, or the body could not be read. |
| `404` | Unknown flow. |
| `409` | The flow's `status` is `stopped`, `paused`, `halted`, or `undeployed`. |
| `413` | Body larger than 10 MiB. |

## 3. Find processed messages

Every message is saved in the configured store with its final status:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/messages?flowId=adt&status=queued'
```

## 4. Statistics and events

Per-flow counters (permission `flows:view`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/flows/adt/stats
```

```json
{"received":3,"filtered":1,"transformed":2,"sent":1,"errored":0,"queued":1,
 "destinations":{"ehr":{"sent":1,"errored":1}},"lastMessageAt":"2026-09-26T12:00:00Z"}
```

- `transformed` counts messages that got past the transform (sent plus queued).
- `destinations` counts successful and failed deliveries per destination.
- `lastMessageAt` is the arrival time of the newest message, `null` until the first one.
- Deleting a flow clears its counters, so a new flow with the same `id` starts at zero.
- Add `?lifetime=true` for lifetime totals. Today both views are identical because no reset exists yet.

Events (permission `events:view`). Each processed message adds one event of type
`message.sent`, `message.queued`, `message.filtered`, or `message.errored`:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/events?flowId=adt&type=message.errored'
```

```json
[{"id":7,"at":"2026-09-26T12:00:00Z","type":"message.errored","flowId":"adt","data":{"messageId":"6f1c…"}}]
```

Both filters are optional. `limit` (1–10000, default 1000) returns the newest matches; results
are oldest first. Events never contain message content or transform error text, because both
can hold patient data. The error is stored with the message instead.

The topology overview (`GET /api/v1/topology`) shows each flow's `received`, `sent`,
`errored`, and `queued` counts under `activity`. Zero counts are included.

## Limits today

- Statistics and events are kept in memory: they restart from zero when the server restarts,
  and only the newest 10,000 events are kept.
- Processing is synchronous and runs once. Failed deliveries are not retried, and `queued`
  messages stay queued.
- Only `http` and `file` destinations are available.
- Messages enter only through this API; flows do not listen on their own ports or read files yet.
- A `file` destination writes wherever `dir` points, with the server's permissions, and an
  `http` destination can target any address the server can reach, including internal ones.
  Only give `flows:edit` to trusted users.
- A flow's `status` is whatever you set when you create it; lifecycle operations (deploy, start,
  stop) do not exist yet.
