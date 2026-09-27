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
reported as `400` with the reason in the [error envelope](api-errors.md), for example:

```json
{"error":{"code":"BAD_REQUEST","message":"invalid flow: dsl: normalize: step 1: build: dsl: step not supported yet"}}
```

Every flow definition you send (create, update, import) is checked against
[`flow.schema.json`](https://github.com/weavster-dev/weavster/blob/main/agent-docs/schemas/flow.schema.json).
Unknown fields and wrong types are rejected rather than ignored:

```text
400 {"error":{"code":"BAD_REQUEST","message":"flow does not match flow.schema.json: /destinations/0/type: value must be one of \"http\", \"file\""}}
```

### `transform`

`transform` is optional. Without it (or with `null` or no `steps`), messages pass through
unchanged and may be any bytes (unless a destination has its own `transform`). It uses the steps
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

### Other fields

| Field | Meaning |
|---|---|
| `id` | Required. 1–128 characters from `A-Z a-z 0-9 . _ -`. `export`, `import`, `redeploy-all`, `connector-names`, `ports-in-use`, `stats`, and `deploy-all`, `undeploy-all`, `start-all`, `stop-all`, `pause-all`, `halt-all`, `resume-all` are reserved. |
| `name`, `sourceType` | Free text shown in lists. |
| `enabled`, `initialState` | Automatic deployment at startup; see [Flow lifecycle](flow-lifecycle.md#enabled-flows-start-automatically). |
| `dependsOn` | Flows this flow requires; see [Flow lifecycle](flow-lifecycle.md). |
| `responseSelector` | The destination whose reply is returned to the sender; see [Return a destination's reply](#return-a-destinations-reply). |
| `source` | Where the flow receives messages on its own; see [Read files from a directory](#read-files-from-a-directory) and [Receive messages over HTTP](#receive-messages-over-http). Without it, messages arrive only through the API. |

### `destinations`

| Field | Meaning |
|---|---|
| `name` | Unique within the flow; 1–128 characters from `A-Z a-z 0-9 . _ -` (it appears in URLs). Used to report delivery results. |
| `type` | `http` (send to `url`) or `file` (write one file per message into `dir`, named by message ID). |
| `url` | Required for `http`: an absolute `http://` or `https://` URL. Each delivery is a request (`POST` unless `method` says otherwise) with `Content-Type: application/json` (transformed messages) or `application/octet-stream` (passthrough). The request carries an `Idempotency-Key` header, the same value for every attempt to deliver this message to this destination, so the receiver can ignore duplicates. |
| `dir` | Required for `file`. Created if missing. |
| `method` | `http` only: `POST` (default), `PUT`, or `PATCH`. |
| `timeoutMs` | `http` only: time allowed for one delivery request, including reading the response, 1000–120000 ms; default 30000. A request that takes longer is a failed attempt and is retried. Stopping or pausing the flow, and stopping the server, wait for deliveries in progress, so keep it as short as the receiver allows. |
| `maxRedirects` | `http` only: how many redirects to follow, 0–10; default 0. See [Redirects](#redirects). |
| `transform` | Optional. This destination's own transform, with the same steps as the flow `transform`. See [Per-destination transforms and filters](#per-destination-transforms-and-filters). |
| `responseTransform` | Optional. Transform applied to this destination's reply. See [Return a destination's reply](#return-a-destinations-reply). |

### Redirects

By default an `http` destination does not follow redirects: a `3xx` reply is a failed delivery,
retried and then dead-lettered like any other failure, so a moved endpoint shows up in the
message's errors (for example `Found: redirect to https://new.example.com/in not followed`)
instead of losing messages. To follow them, set `maxRedirects`:

```json
{"name": "ehr", "type": "http", "url": "https://ehr.example.com/in", "method": "PUT",
 "timeoutMs": 10000, "maxRedirects": 2}
```

- Only `307` and `308` redirects are followed; they repeat the same method and body.
- `301`, `302`, and `303` are never followed, because clients turn them into a `GET` without the
  message: the delivery fails with that status. Update `url` to the new address instead.
- A redirect from `https://` to `http://` is never followed.
- Each redirect counts toward `maxRedirects`; one more is a failed delivery.

### Per-destination transforms and filters

A destination's `transform` runs on the flow's output, just before delivery to that
destination. The other destinations are not affected. A `filter` step in it drops the message
for that destination only:

```json
"destinations": [
  {"name": "ehr", "type": "http", "url": "https://ehr.example.com/inbound",
   "transform": {"steps": [{"filter": {"when": "patient.lastName == ''", "action": "reject"}}]}},
  {"name": "archive", "type": "file", "dir": "/var/lib/weavster/archive",
   "transform": {"steps": [{"set": {"field": "archivedBy", "expr": "weavster"}}]}}
]
```

- The destination receives the result as `application/json`.
- A destination whose filter drops the message is not delivered to and counts as done. A
  [stopped](flow-lifecycle.md#stopping-one-destination) destination still holds the message
  until you start it; its filter is checked then. The
  message is `sent` once the other destinations succeed. It is `filtered` when every destination
  drops it.
- Once any destination has a `transform`, every message sent to the flow must be a JSON object
  (`400` otherwise), even when the flow itself has no `transform`.
- A destination transform that fails (for example `"x" is not a number`) counts as a failed
  delivery to that destination. It is retried and then dead-lettered like any other failure.
  Because the same input gives the same failure, fix the transform with an update: `queued`
  messages use the new definition.
- Retries, and deliveries after a restart, run the destination's current transform again on
  the stored flow output.

### Return a destination's reply

Set `responseSelector` to a destination name to get that destination's reply back in the
response to [`POST /api/v1/flows/{id}/messages`](#3-send-a-message). Add a
`responseTransform` to that destination to reshape the reply first (same steps as `transform`):

```json
{
  "id": "adt",
  "responseSelector": "ehr",
  "destinations": [
    {"name": "ehr", "type": "http", "url": "https://ehr.example.com/inbound",
     "responseTransform": {"steps": [{"map": {"from": "code", "to": "ack"}}]}},
    {"name": "archive", "type": "file", "dir": "/var/lib/weavster/archive"}
  ]
}
```

```json
{"id":"6f1c…","status":"sent","response":{"ack":"AA","code":"AA"}}
```

- The reply is the HTTP response body of a successful delivery. `responseSelector` must name
  one of the flow's `http` destinations; naming an unknown or `file` destination returns `400`.
- `responseTransform` is only allowed on the `responseSelector` destination (`400` elsewhere).
- `response` is present only when the selected destination was delivered to while your request
  waited. Retries in the background return nothing.
- Without a `responseTransform`, a reply whose `Content-Type` is JSON (`application/json` or
  `…+json`) is returned as is. Any other reply is returned as a JSON string, for example
  `"MSA|AA|123"`.
- With a `responseTransform`, the reply must be a JSON object. If it is not, or a step fails, or
  a `filter` step drops it, `response` is left out.
- `response` is also left out when the reply is empty, larger than 1 MiB, or cut off. In every
  one of these cases the delivery still counts as successful.

### Update a flow

Replace a flow's definition, or rename it by changing `name` (permission `flows:edit`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X PUT http://127.0.0.1:8080/api/v1/flows/adt -d '{
  "name": "ADT normalize v2", "sourceType": "http", "enabled": true,
  "transform": {"kind": "Transform", "name": "normalize", "inputs": ["message"], "steps": [ … ]},
  "destinations": [ … ]
}'
```

- The body replaces the whole definition: fields you leave out are cleared. There are two
  exceptions: the flow keeps its `status`, and it keeps `enabled` unless you include that field.
- Messages received after the update use the new definition. A running flow does not need a
  restart.
- `queued` messages are retried against the current definition. A changed destination URL is
  used for them, and a removed destination is no longer retried for them.
- Statistics of a removed or renamed destination remain listed until the server restarts.
- The same checks as create apply (`400` with the reason).
- `id` comes from the URL: a different `id` in the body, or any `status` field, returns `400`.
- Unknown flows return `404`.

### Update several flows at once

`PUT /api/v1/flows` replaces several definitions in one request (permission `flows:edit`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X PUT http://127.0.0.1:8080/api/v1/flows -d '{
  "flows": [
    {"id": "adt", "name": "ADT normalize v3", "sourceType": "http", "destinations": [ … ]},
    {"id": "orm", "name": "Orders", "enabled": false}
  ]
}'
```

```json
{"updated":["adt","orm"]}
```

- Each entry follows the same rules as `PUT /api/v1/flows/{id}`: it replaces the whole
  definition, and the flow keeps its `status`, its stopped destinations, and `enabled`
  (unless the entry sets it).
- Every entry is checked first. If any is invalid (`400`) or any flow does not exist (`404`,
  naming the missing ids), nothing is written.
- Flows are written dependencies first. If the store fails part-way, the response is `500` with
  `{"error":{"code":"UPDATE_INCOMPLETE",…},"updated":[…]}` listing the flows already written.
- The body is `{"flows":[…]}` with no other top-level field (`400` otherwise) and may be up
  to 50 MiB (`413` above that).

### Connector names

List every flow's source type and destination names (permission `flows:view`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/flows/connector-names
```

```json
[{"id":"adt","name":"ADT normalize","sourceType":"http","destinations":["ehr","archive"]}]
```

### Ports in use

List the ports the server listens on (permission `flows:view`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/flows/ports-in-use
```

```json
[{"address":"127.0.0.1:8080","port":8080,"usedBy":"api"},{"address":":8443","port":8443,"usedBy":"api-tls"}]
```

`usedBy` is `api` for `listen.address`, `api-tls` for `listen.tlsAddress`, and `flow:<id>` for
the open port of a started flow's [http source](#receive-messages-over-http).

### Read files from a directory

A flow with a file `source` picks up the files that appear in a directory, sends each one through
the flow as a message, and then deletes it (or moves it into `moveTo`):

```json
{
  "id": "adt",
  "source": {"type": "file", "dir": "/var/lib/weavster/in/adt", "pattern": "*.json",
             "pollIntervalMs": 1000, "moveTo": "/var/lib/weavster/done/adt"},
  "destinations": [{"name": "ehr", "type": "http", "url": "https://ehr.example.com/in"}]
}
```

| Field | Meaning |
|---|---|
| `type` | `file`. |
| `dir` | Required. Absolute directory to read. It does not have to exist yet. |
| `pattern` | File-name glob (`*.json`, `ADT_*.hl7`); default `*`. No path separators. |
| `pollIntervalMs` | How often the directory is read: 100–3600000, default 1000. |
| `moveTo` | Absolute directory processed files are moved into (created if missing; a name that is already there gets the message id appended). Without it, processed files are deleted. |

- The directory is read only while the flow is `started`. Stopping, pausing, or deleting the flow
  stops reading; starting it again picks up what arrived meanwhile.
- Only regular files directly in `dir` are read: subdirectories, symbolic links, and hidden files
  (names starting with `.`, such as `rsync` temporary files and `.DS_Store`) are skipped. A
  pattern starting with `.` reads hidden files.
- A directory can be read by one flow only; a second flow with the same `dir` is refused.
- Each poll reads at most 100 files, so one busy directory does not hold up other flows; the
  rest are read at the next poll.
- A file is read once it has not changed for a second, so a file still being written is not taken
  half-way. Writing to a temporary name and renaming it into `dir` is safest.
- Files are read in name order. Each message has the metadata `source.file` with the file name.
- A file becomes a message exactly like one sent with the API (the same checks, transform, and
  delivery). After the message is stored, the file is deleted or moved. If the server stops
  between the two, the file is read again when it starts: delivery is at least once, so
  destinations should tolerate a repeat. If the file cannot be deleted or moved (for example
  the server may read but not write the directory), it is logged and not read again until it
  changes.
- A file the flow refuses (larger than 10 MiB, or not a JSON object when the flow has a transform)
  is moved into `moveTo/rejected`, or, without `moveTo`, left where it is and skipped until it
  changes. Either way a `source.file.rejected` [event](#5-statistics-and-events) records the file
  and the reason.
- A directory that cannot be read is logged (once per distinct error) and read again at the next
  interval. A file that cannot be read is logged once and skipped until it changes.
- `dir` must not be `moveTo/rejected`, where refused files go.
- The server reads and moves files with its own permissions; only give `flows:edit` to users you
  trust with the directories it can reach.

### Receive messages over HTTP

A flow with an http `source` listens on its own address while it is started. Each request with
the configured method and path is a message:

```json
{
  "id": "adt",
  "source": {"type": "http", "address": "127.0.0.1:9001", "path": "/adt", "method": "POST"},
  "destinations": [{"name": "ehr", "type": "http", "url": "https://ehr.example.com/in"}]
}
```

| Field | Meaning |
|---|---|
| `type` | `http`. |
| `address` | Required. `host:port` to listen on, for example `127.0.0.1:9001`, or `:9001` for every interface. The port must be a number from 1 to 65535. |
| `path` | Request path accepted; default `/`. Must start with `/`. |
| `method` | `POST` (default) or `PUT`. |
| `username`, `passwordEnv` | Optional, together: senders must use HTTP Basic authentication with this user name (no `:`, which separates user and password in Basic authentication) and the password in the server's environment variable `passwordEnv`. The variable name must start with `WEAVSTER_SOURCE_` followed by capital letters, digits, or `_`, so a flow cannot use the server's other secrets. The password never goes into the flow definition. |
| `readTimeoutMs` | Time allowed to read one request, headers and body, 1000–600000 ms; default 60000. A sender slower than that gets its connection closed. |
| `certFile`, `keyFile` | Optional, together: absolute paths of a PEM certificate chain and private key on the server. The port then serves HTTPS only (HTTP/1.1 and HTTP/2), with the server's TLS settings (`tls.minVersion`). The server's own `tls.keyFile` is refused: give each flow its own certificate. |

Once the flow is started, send it a message:

```bash
curl -s -X POST http://127.0.0.1:9001/adt -d '{"PID":{"5":{"1":"Doe"}}}'
```

```json
{"id":"6f1c…","status":"sent"}
```

- The request body goes through the same checks, transform, and delivery as a message sent with
  [`POST /api/v1/flows/{id}/messages`](#3-send-a-message), with the same 10 MiB limit and the same
  success reply (`202` with the message id and status, plus `response` with a
  `responseSelector`). Only the error codes below differ, because they are meant for a sending
  system. Each message has the metadata `source.http.path`.
- Other paths get `404`, other methods `405` with an `Allow` header, a body over 10 MiB `413`, and
  a message the flow refuses (for example not a JSON object when the flow has a transform) `400`.
  While the flow is stopping, or after it was removed, requests get `503`.
- A message stored before a later failure is still answered `202` (the API answers `500`): the
  flow has it, and resending it would store it twice. A flow that is not running answers `503`
  (the API answers `409`), so the sender tries again later.
- The port is open only while the flow is `started`. Starting, stopping, pausing, changing, or
  deleting the flow opens or closes it within about a second; stopping the server closes it.
- A port can have one flow source; a second flow with the same port, or one using the port of
  `listen.address` or `listen.tlsAddress`, is refused. A port that
  cannot be opened (for example another program or the API uses it) is logged and recorded as a
  `source.http.failed` [event](#5-statistics-and-events) with the address, and tried again every
  second.
- `weavster flow ports` (`GET /api/v1/flows/ports-in-use`) lists the open flow ports as
  `flow:<id>`.
- Without `username`, anyone who can reach the address can send the flow messages, and without
  `certFile` messages (and the Basic password) cross the network unencrypted. Use both whenever
  the port is reachable from other machines, or listen on `127.0.0.1` only.

#### Require a password and HTTPS

```bash
export WEAVSTER_SOURCE_LAB_PASSWORD='a long random password'
weavster server --config weavster.yaml
```

```json
{
  "id": "lab",
  "source": {"type": "http", "address": ":9443", "path": "/results",
             "username": "lab", "passwordEnv": "WEAVSTER_SOURCE_LAB_PASSWORD",
             "certFile": "/etc/weavster/tls/lab.crt", "keyFile": "/etc/weavster/tls/lab.key"}
}
```

```bash
curl -s -u 'lab:a long random password' https://weavster.example.com:9443/results -d '{"result":"ok"}'
```

- A request without the right user name and password gets `401` with a
  `WWW-Authenticate: Basic` header, before the path or method is checked.
- The password and certificate are read when the port opens. After changing the variable (restart
  the server) or replacing the certificate files, stop and start the flow to use them.
- If the variable is not set (or empty), or the certificate cannot be loaded, the port stays
  closed: the reason is logged and recorded in a `source.http.failed` event (field `reason`; a
  new reason is recorded again), and opening is tried again every second.
- The server reads the certificate and key files with its own permissions; only give
  `flows:edit` to users you trust with the files it can read.

## 2. Deploy and start the flow

A new flow is `undeployed` and rejects messages until you deploy and start it (permission
`flows:deploy`). See [Flow lifecycle](flow-lifecycle.md).

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/deploy
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/start
```

## 3. Send a message

You need the `messages:send` permission (or `admin`).

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  http://127.0.0.1:8080/api/v1/flows/adt/messages -d '{"PID":{"5":{"1":"Doe"}}}'
```

```json
{"id":"6f1c…","status":"sent"}
```

The request returns after processing finishes. With a `responseSelector`, the response also
carries that destination's reply as `response` (see
[Return a destination's reply](#return-a-destinations-reply)). `status` is one of:

| Status | Meaning |
|---|---|
| `sent` | Delivered to every destination. |
| `queued` | At least one destination failed, or is [stopped](flow-lifecycle.md#stopping-one-destination). It is retried automatically (see [Retries](#retries)). |
| `dead-lettered` | A destination still failed after `delivery.maxAttempts` attempts and no other destination has work left, or the flow was deleted while the message was queued. Not retried again. |
| `filtered` | A flow `filter` step dropped the message, or every destination's own filter did. Nothing was delivered. |
| `errored` | The transform failed (for example `"x" is not a number`). Nothing was delivered. |

Processing continues even if your client disconnects, so every message ends in one of the
statuses above.

Errors:

| Response | Cause |
|---|---|
| `400` | The flow or one of its destinations has a `transform` (a `responseTransform` does not count) and the body is not a JSON object, or the body could not be read. |
| `404` | Unknown flow. |
| `409` | The flow is not `started`. |
| `413` | Body larger than 10 MiB. |

## 4. Find processed messages

Every message is saved in the configured store with its final status. Search them (permission
`messages:view`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/messages?flowId=adt&status=queued&limit=50'
```

```json
[{"id":"6f1c…","flowId":"adt","status":"queued","contentType":"json",
  "receivedAt":"2026-09-26T12:00:00Z","updatedAt":"2026-09-26T12:00:05Z",
  "attempts":{"ehr":{"attempts":2,"lastError":"Service Unavailable","nextAttemptAt":"2026-09-26T12:00:09Z"}}}]
```

| Parameter | Meaning |
|---|---|
| `flowId`, `status` | Only messages of this flow / with this status. |
| `from`, `to` | Received at or after / at or before this time (RFC 3339, for example `2026-09-26T12:00:00Z`); `from` must not be after `to`. |
| `limit` | Messages per page, 1–1000 (default 100). |
| `offset` | Messages to skip, for the next pages. |
| `sort` | `-receivedAt` (newest first, default), `receivedAt`, `id`, or `-id`. |

The filters are applied before `limit` and `offset`, so every page holds only matching messages,
and messages received in the same instant are ordered by id, so pages neither repeat nor skip
messages while no new ones arrive.

### Message trends

`GET /api/v1/messages/trends` (permission `messages:view`) counts the messages received in each
hour or day of a range, by their current status. It returns counts only, never content.

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/messages/trends?from=2026-09-27T10:00:00Z&to=2026-09-27T12:00:00Z&flowId=adt'
```

```json
[{"start":"2026-09-27T10:00:00Z","total":0,"statuses":{"dead-lettered":0,"errored":0,"filtered":0,"queued":0,"received":0,"sent":0,"transformed":0}},
 {"start":"2026-09-27T11:00:00Z","total":3,"statuses":{"dead-lettered":0,"errored":1,"filtered":0,"queued":0,"received":0,"sent":2,"transformed":0}}]
```

| Parameter | What it does |
|---|---|
| `from`, `to` | Required (RFC 3339, to the millisecond). Buckets start at `from`; `to` is not included (unlike the message search), so ranges that meet, such as one day and the next, never count a message twice. The last bucket may be shorter. |
| `interval` | `hour` (default) or `day`: the bucket length. |
| `flowId` | Only that flow's messages; an unknown flow returns `404`. |

Every bucket and every status is listed, zeros included, so you can chart the result as it is.
A range of more than 1000 buckets is refused with `400`; use `interval=day` or a shorter range.
The counts come from the stored messages, so removed messages no longer count and each message
counts once, in its current status.

### Work with one message

| Request | Permission | What it does |
|---|---|---|
| `GET /api/v1/messages/{id}` | `messages:view` | The message as in the search results. |
| `GET /api/v1/messages/{id}/content?part=raw` | `messages:content` | The content as received (`part=transformed`: after the flow transform), as bytes. A message with no transformed content (for example one that errored in its transform) returns `404` for `part=transformed`. |
| `POST /api/v1/messages/{id}/requeue` | `messages:view`, `messages:send` | Gives a dead-lettered message another round of delivery attempts; see [Dead-lettered messages](#dead-lettered-messages). |
| `POST /api/v1/messages/{id}/reprocess` | `messages:send` | Sends the original content through the message's flow again. The new message (`202`, same reply as sending) keeps the old message's metadata (except its `error`) and adds `reprocessedFrom` with the old id. The flow must be `started` (`409` otherwise). |
| `DELETE /api/v1/messages/{id}` | `messages:delete` | Removes the message (`204`). A message that is being processed or retried right now returns `409`; try again. |

An unknown id returns `404`. Message content can hold protected health information, so
`messages:content` is a separate permission and every search, read, and content request is
recorded in the [audit log](audit-log.md) as `phi.access`.

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/messages/6f1c…/content?part=raw'
```

### Remove many messages

`DELETE /api/v1/messages` (permission `messages:delete`) removes every message that matches the
search filters `flowId`, `status`, `from`, and `to`. There is no limit: every match is removed.

```bash
# Every errored message of one flow
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X DELETE \
  'http://127.0.0.1:8080/api/v1/messages?flowId=adt&status=errored'
```

```json
{"deleted":12,"busy":0,"restarted":[]}
```

| Parameter | What it does |
|---|---|
| `all=true` | Required when you give no filter: removes every message. Without it, a request with no filter returns `400`, so a missing parameter never clears the store by accident. |
| `restart=true` | Stops the started flows first (only the `flowId` flow, when given), removes the messages, then starts those flows again. `restarted` lists them. Stopping waits for the messages those flows are processing, so they are removed too. Paused and halted flows are left as they are. |

- A message being processed or retried at that moment is kept and counted in `busy`. Run the
  request again, or use `restart=true`.
- Each message is checked against the filters again just before it is removed, so one that
  changed meanwhile (for example a `queued` message that was delivered) is kept.
- `limit`, `offset`, and `sort` are refused (`400`), because they would suggest that only part
  of the matches is removed.
- If a flow cannot be stopped, nothing is removed and the flows already stopped are started
  again (`409`).
- If a flow does not start again afterwards, the reply is `500` and names every such flow; start
  them yourself with `POST /api/v1/flows/{id}/start`.
- If removal fails part-way, the reply is `500` and says how many messages were removed; run
  the request again to remove the rest.

To clear everything from the command-line client, use `clearallmessages`.

### Export and import messages

Export writes the messages that match the search parameters into one gzipped archive (permission
`messages:content`, audited). An archive holds complete messages: the content as received and after
the transform, status, times, attempts, and metadata. It takes up to 10,000 messages, the newest
first unless you set `sort`; with `limit` and `offset` you export larger sets in parts:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -o adt.json.gz \
  'http://127.0.0.1:8080/api/v1/messages/export?flowId=adt'
```

The reply header `Weavster-Message-Count` says how many messages the archive holds; when it is
10,000 there may be more, so export the next part with `offset=10000`.

Import restores an archive (permission `messages:import`, up to 100 MiB):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' --data-binary @adt.json.gz \
  'http://127.0.0.1:8080/api/v1/messages/import?flowId=adt'
```

```json
{"imported":120,"skipped":3,"busy":0}
```

- Messages keep their id, status, receive time, attempts, metadata, and content; their update time
  becomes the time of the import. A message whose id
  already exists is `skipped`; add `overwrite=true` to replace it. A message the server is
  processing at that moment is left as it is and counted as `busy`.
- Every flow the messages belong to must exist, or nothing is imported (`404` naming the flow).
  `flowId` assigns every imported message to that flow instead.
- Imported messages that are `queued` (or were still being processed) are picked up by the retry
  worker when their flow is `started`, and delivered. Import into a stopped flow to keep them as
  history.
- An archive that is not gzip, not an export, encrypted with another key, or larger than 512 MiB
  once decompressed returns `400`.
- If writing fails part-way, the reply is `500 {"error":{"code":"IMPORT_INCOMPLETE",…},"imported":…}`
  with what was already written; importing the same archive again skips those messages.

To encrypt an archive, send a key in the `Weavster-Archive-Key` header on export, and the same key
on import. The key is 32 random bytes in base64; keep it safe, because the archive cannot be read
without it. An encrypted archive is `application/octet-stream` (it is compressed before encryption):

```bash
KEY=$(openssl rand -base64 32)
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -H "Weavster-Archive-Key: $KEY" -o adt.enc.gz \
  'http://127.0.0.1:8080/api/v1/messages/export?flowId=adt'
```

## 5. Statistics and events

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
- `lastMessageAt` is the arrival time of the newest message; it is left out until the first one.
- Deleting a flow clears its counters, so a new flow with the same `id` starts at zero.
- Add `?lifetime=true` for lifetime totals, which are kept when you reset the current counters.
- `GET /api/v1/flows/stats` returns every flow's statistics at once, keyed by flow id (also with
  `?lifetime=true`).

Reset statistics (permission `flows:deploy`):

```bash
# One flow's current counters
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/flows/adt/stats/reset
# Every flow's current counters and lifetime totals
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST 'http://127.0.0.1:8080/api/v1/flows/stats/reset?lifetime=true'
```

Both return `204`. Without `lifetime=true` only the current counters are cleared; the lifetime
totals keep counting. An unknown flow returns `404`.

Events (permission `events:view`). Each processed message adds one event of type
`message.sent`, `message.queued`, `message.filtered`, or `message.errored`:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/events?flowId=adt&type=message.errored'
```

```json
[{"id":7,"at":"2026-09-26T12:00:00Z","type":"message.errored","flowId":"adt","data":{"messageId":"6f1c…"}}]
```

Every filter is optional:

| Parameter | What it does |
|---|---|
| `type`, `flowId` | Only events of that type or flow. |
| `from`, `to` | Only events at or after / at or before that time (RFC 3339); `from` must not be after `to`. |
| `afterId` | Only events with a larger id. |
| `limit` | How many matches, 1–10000 (default 1000): the newest ones, or with `afterId` the oldest ones after it. |

Results are oldest first. Events never contain message content or transform error text, because
both can hold patient data. The error is stored with the message instead.

| Request | What it returns |
|---|---|
| `GET /api/v1/events/{id}` | One event; `404` if unknown or no longer kept. |
| `GET /api/v1/events/count` | `{"count": n}` for the same filters (no limit). |
| `GET /api/v1/events/max-id` | `{"maxId": n}`: the newest event's id, `0` when there is none. |
| `GET /api/v1/events/export` | Every match (no limit) as a JSON file download (`events.json`). |

To follow new events, remember the last id you saw (or start from `max-id`) and ask for the ones
after it. With `afterId`, the reply holds the oldest events after that id, so when more than
`limit` arrived, ask again with the last id of the reply until it comes back short; nothing is
skipped:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/events?afterId=41'
```

The server keeps the newest 10,000 events in memory: older ones are dropped, and the log starts
empty when the server restarts.

To save statistics or events to a file from the command-line client, use `dump stats "path"` or
`dump events "path"` (the newest 10,000 events).

The topology overview (`GET /api/v1/topology`) shows each flow's `received`, `sent`,
`errored`, and `queued` counts under `activity`. Zero counts are included.

### Statistics over time

The server samples every flow's lifetime totals when it starts and then once a minute, and keeps
the samples for 24 hours
(change both with [`stats`](server-config.md#stats) in the server configuration).
`GET /api/v1/stats/series` (permission `flows:view`) returns them, oldest first:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/stats/series?flowId=adt&from=2026-09-27T10:00:00Z'
```

```json
[{"at":"2026-09-27T10:00:30Z","flowId":"adt","stats":{"received":3,"filtered":1,"transformed":2,"sent":1,"errored":0,"queued":1,"destinations":{"ehr":{"sent":1,"errored":1}},"lastMessageAt":"2026-09-26T12:00:00Z"}},
 {"at":"2026-09-27T10:01:30Z","flowId":"adt","stats":{"received":5,"filtered":1,"transformed":4,"sent":3,"errored":0,"queued":1,"destinations":{"ehr":{"sent":3,"errored":1}},"lastMessageAt":"2026-09-27T10:01:12Z"}}]
```

- Each sample has the same `stats` as `GET /api/v1/flows/{id}/stats?lifetime=true`. The
  difference between two samples is the traffic in between.
- `flowId`, `from`, and `to` (RFC 3339, both inclusive) are optional; without `flowId` every
  flow's samples are returned, interleaved by time.
- At most `limit` samples are returned (default 1000, up to 10000): the newest ones that match,
  still oldest first. Narrow with `flowId` or `from` to reach older samples.
- Samples are kept in memory: they start again after a restart, and a lifetime reset shows as a
  drop to zero. Deleting a flow drops its samples, so a new flow with the same `id` starts a new
  series. The server keeps at most 1,000,000 samples in total and drops the oldest past that.
- An unknown `flowId` returns `404`; a bad time, `from` after `to`, or a `limit` outside 1–10000
  returns `400`.
- For message counts by status over hours or days from the stored messages, use
  [message trends](#message-trends) instead.

## Retries

A failed destination is retried in the background. The delay before retry *n* is
`delivery.backoffBaseMs × 2^(n-1)`, capped at one minute. The check for due retries runs every
`delivery.retryIntervalMs`. Only the failed destinations are retried, and every retry sends the
same `Idempotency-Key`, so an HTTP receiver can ignore duplicates. When a destination has failed
`delivery.maxAttempts` times (default 5), the message becomes `dead-lettered` and a
`message.dead-lettered` event is logged. See [Server configuration](server-config.md#delivery).

Retry times are stored with the message. After a restart, the server resumes pending
retries right away. With `store.dialect: sqlite`, a message that was `queued` when the server
stopped is delivered once its destination is back. So is a message the server was still
processing when it stopped or crashed: it is transformed if needed and delivered to every
destination that has not received it.

On SIGINT or SIGTERM the server stops accepting connections on every listener and stops the
retry worker once its current message is done. It then waits up to `listen.shutdownTimeoutMs`
(default 10 seconds) for requests in progress and that message. At the deadline it closes the
remaining connections and exits, logging a warning. A message still being delivered at that
point is already stored. The next start finishes it and sends the same `Idempotency-Key`, so a
destination that already received it can ignore the repeat. A second SIGINT/SIGTERM during the
wait stops the server immediately.

Messages of a flow that is not `started` are not retried; they stay `queued`.

A retried message that later succeeds changes to `sent`. The statistics then count it once as
`queued` and once as `sent`.

## Dead-lettered messages

A message is `dead-lettered` when a destination still failed after `delivery.maxAttempts`
attempts (see [Retries](#retries)). It is kept, with each destination's attempts and last error,
until you requeue or remove it.

From the [command-line client](cli.md):

```text
weavster> deadletter list adt
7f3c9a1e	adt	2026-09-27T10:00:00Z	archive: delivered; ehr: 5 attempts, POST https://ehr.example.com/in: 503 Service Unavailable
1 dead-lettered messages
weavster> deadletter show 7f3c9a1e
{ "id": "7f3c9a1e", "flowId": "adt", "status": "dead-lettered", "attempts": { ... }, ... }
weavster> deadletter requeue 7f3c9a1e
requeued 7f3c9a1e (before: archive: delivered; ehr: 5 attempts, POST https://ehr.example.com/in: 503 Service Unavailable)
weavster> deadletter requeue all adt
requeued 12 messages, skipped 0
weavster> deadletter remove 7f3c9a1e
removed 7f3c9a1e
```

`deadletter list` without a flow lists every flow's dead letters, newest first; it shows at most
1000 and says so when there may be more. `requeue all` without a flow requeues every dead letter.

**Requeue** gives the message another round of attempts:

- Every destination that did not deliver starts again with no attempts; destinations that
  delivered keep their record and are **not sent again**. The message becomes `queued`, and the
  retry loop delivers it with the usual backoff, so fix the destination first.
- The attempts the message had are kept: the reply shows each destination's attempts and last
  error; the [audit log](audit-log.md) and a `message.requeued` [event](#5-statistics-and-events)
  record the attempt counts (`previous.<destination>.attempts`) but not the error text, which can
  quote message content. The message's `requeues` metadata counts its requeues.
- A message that was never transformed (its flow was deleted before that) goes back to
  `received`, so it is transformed before it is delivered.
- Statistics count each outcome: a requeued message that is then sent counts once as errored
  (when it was dead-lettered) and once as sent.
- Only a dead-lettered message can be requeued (`409` otherwise, and while it is being
  processed). A message whose flow was deleted cannot be requeued (`404`); send its content again
  to another flow instead.
- `requeue all` skips messages it cannot requeue and lists them with the reason.

With the API (permission `messages:send`; requeuing one message also needs `messages:view`,
because the reply shows the previous errors, and is logged as PHI access):

```bash
# One message
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/messages/7f3c9a1e/requeue
# Every dead letter of a flow (omit flowId for all flows)
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST 'http://127.0.0.1:8080/api/v1/messages/requeue?flowId=adt'
```

```json
{"message":{"id":"7f3c9a1e","flowId":"adt","status":"queued","metadata":{"requeues":"1"},"attempts":{"archive":{"attempts":1}}},
 "previous":{"archive":{"attempts":1},"ehr":{"attempts":5,"lastError":"POST https://ehr.example.com/in: 503 Service Unavailable"}}}
```

```json
{"requeued":["7f3c9a1e","8a01b2c3"],"skipped":[]}
```

Listing and inspecting use the message endpoints `GET /api/v1/messages?status=dead-lettered` and
`GET /api/v1/messages/{id}` (see [Work with one message](#work-with-one-message)). Removing uses
`DELETE /api/v1/messages/{id}?status=dead-lettered` (permission `messages:delete`), which deletes
the message only if it is still dead-lettered (`409` otherwise), so a mistyped id or a message
someone just requeued is never removed; `deadletter remove` uses it. To send the original content
again as a new message instead, use `reprocess`.

## Limits today

- Statistics and events are kept in memory: they restart from zero when the server restarts,
  and only the newest 10,000 events are kept.
- The first delivery attempt runs while your request waits; retries run in the background.
- Only `http` and `file` destinations are available.
- Besides this API, messages enter only through [file sources](#read-files-from-a-directory) and
  [http sources](#receive-messages-over-http); TCP/MLLP and database sources are not available
  yet.
- A `file` destination writes wherever `dir` points, with the server's permissions, and an
  `http` destination can target any address the server can reach, including internal ones.
  Only give `flows:edit` to trusted users.
