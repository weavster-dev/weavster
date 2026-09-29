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
reported as `400` with the reason in the [error envelope](api-errors.md), for example for a
`filter` whose `when` is `patient.lastName ==`, with nothing to compare against:

```json
{"error":{"code":"BAD_REQUEST","message":"invalid flow: dsl: normalize: step 2: filter.when: empty path"}}
```

Every flow definition you send (create, update, import) is checked against
[`flow.schema.json`](https://github.com/weavster-dev/weavster/blob/main/agent-docs/schemas/flow.schema.json).
Unknown fields and wrong types are rejected rather than ignored:

```text
400 {"error":{"code":"BAD_REQUEST","message":"flow does not match flow.schema.json: /destinations/0/type: value must be one of \"http\", \"file\""}}
```

### `transform`

`transform` is optional. Without it (or with `null` or no `steps`), messages pass through
unchanged and may be any bytes (unless a destination has its own `transform`, or `inputFormat`
is not `json`). It uses the steps below, in order, on the message as a JSON object, or on the
JSON view of an [HL7 v2 message](#transform-hl7-v2-messages),
[XML document](#transform-xml-documents), or [delimited text](#transform-delimited-text-csv).

| Step | Fields | Effect |
|---|---|---|
| `map` | `from`, `to`, optional `type` (`string`, `number`, `boolean`) | Copies the value at `from` to `to`, converting it if `type` is set. A missing `from` leaves `to` unchanged. |
| `set` | `field`, `expr` | Sets `field` to `expr`, replacing each `{{path}}` with that value (an empty string when missing). |
| `filter` | `when`, `action` (`reject` or `accept`) | `reject` drops the message when `when` is true; `accept` drops it when `when` is false. |
| `build` | `template`, optional `format` (`json`, `hl7v2`, `xml`, `text`) | Renders the output from `template`, replacing each `{{path}}`. Last step only; see [Build the output](#build-the-output-build). |
| `destinationSet` | `exclude` (destination names), optional `when` | Excludes those destinations for this message when `when` is true (always without `when`). Flow `transform` only; see [Route by content](#route-by-content-destinationset). |

- **Paths** use dots: `patient.lastName`. A number indexes an array: `items.0.code`.
- **`when`** is either a path (true when the value is present and not empty, `0`, `false`, or `null`),
  or `<operand> == <operand>` / `<operand> != <operand>`. An operand is a path, a quoted string
  (`'x'` or `"x"`), a number (`3`, `-1.5`), or `true`/`false`. A missing path compares equal
  to `''`. No other operators exist.
- Numbers keep their exact digits (for example 20-digit identifiers) unless a step converts them.

The transform format is published as
[`transform.schema.json`](https://github.com/weavster-dev/weavster/blob/main/agent-docs/schemas/transform.schema.json).
The server checks every flow, destination, and response transform against it, and so does
`weavster config validate`, so a mistake is reported with its place in the document:

```text
400 {"error":{"code":"BAD_REQUEST","message":"flow does not match flow.schema.json: /transform/steps/0/filter/action: value must be one of \"reject\", \"accept\""}}
```

Editors that understand JSON Schema can check transforms as you type. For a config-as-code YAML
file (its flows' transforms included), put this on the first line; with the YAML extension for
VS Code, for example, mistakes are underlined and fields are completed:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/weavster-dev/weavster/main/agent-docs/schemas/config.schema.json
```

### Other fields

| Field | Meaning |
|---|---|
| `id` | Required. 1–128 characters from `A-Z a-z 0-9 . _ -`. `export`, `import`, `redeploy-all`, `connector-names`, `ports-in-use`, `stats`, and `deploy-all`, `undeploy-all`, `start-all`, `stop-all`, `pause-all`, `halt-all`, `resume-all` are reserved. |
| `name`, `sourceType` | Free text shown in lists. |
| `enabled`, `initialState` | Automatic deployment at startup; see [Flow lifecycle](flow-lifecycle.md#enabled-flows-start-automatically). |
| `dependsOn` | Flows this flow requires; see [Flow lifecycle](flow-lifecycle.md). Flows it [sends to](#send-to-another-flow) count too. |
| `responseSelector` | The destination whose reply is returned to the sender; see [Return a destination's reply](#return-a-destinations-reply). |
| `inputFormat` | How transforms read a message: `json` (default), `hl7v2`, `xml`, or `delimited`; see [Transform HL7 v2 messages](#transform-hl7-v2-messages), [Transform XML documents](#transform-xml-documents), and [Transform delimited text](#transform-delimited-text-csv). |
| `delimited` | Options of `inputFormat: delimited`: `delimiter` and `header`. |
| `source` | Where the flow receives messages on its own; see [Read files from a directory](#read-files-from-a-directory), [Receive messages over HTTP](#receive-messages-over-http), [Receive HL7 v2 over MLLP](#receive-hl7-v2-over-mllp), and [Read rows from a database](#read-rows-from-a-database). Without it, messages arrive only through the API. |

### `destinations`

| Field | Meaning |
|---|---|
| `name` | Unique within the flow; 1–128 characters from `A-Z a-z 0-9 . _ -` (it appears in URLs). Used to report delivery results. |
| `type` | `http` (send to `url`), `file` (write one file per message into `dir`, named by message ID), `mllp` (send HL7 v2 to `address` over TCP; see [Send HL7 v2 over MLLP](#send-hl7-v2-over-mllp)), `flow` (hand the message to another flow; see [Send to another flow](#send-to-another-flow)), or `database` (insert a row into a table; see [Write rows to a database](#write-rows-to-a-database)). |
| `url` | Required for `http`: an absolute `http://` or `https://` URL. Each delivery is a request (`POST` unless `method` says otherwise) with `Content-Type: application/json` (transformed messages), the [`build`](#build-the-output-build) format's type, or `application/octet-stream` (passthrough). The request carries an `Idempotency-Key` header, the same value for every attempt to deliver this message to this destination, so the receiver can ignore duplicates. |
| `dir` | Required for `file`: an absolute path, created if missing. |
| `address` | Required for `mllp`: `host:port` of the receiving system, for example `lab.example.com:2575`. |
| `flow` | Required for `flow`: the id of the flow that gets the message. |
| `method` | `http` only: `POST` (default), `PUT`, or `PATCH`. |
| `timeoutMs` | `http` and `mllp`: time allowed for one delivery, including reading the response or ACK, 1000–120000 ms; default 30000. A request that takes longer is a failed attempt and is retried. Stopping or pausing the flow, and stopping the server, wait for deliveries in progress, so keep it as short as the receiver allows. |
| `maxRedirects` | `http` only: how many redirects to follow, 0–10; default 0. See [Redirects](#redirects). |
| `transform` | Optional. This destination's own transform, with the same steps as the flow `transform`. See [Per-destination transforms and filters](#per-destination-transforms-and-filters). |
| `responseTransform` | Optional. Transform applied to this destination's reply. See [Return a destination's reply](#return-a-destinations-reply). |

### Transform HL7 v2 messages

Set `"inputFormat": "hl7v2"` and the flow's transforms read each HL7 v2 message as JSON, so
paths name segment, field, and component the way HL7 does:

```json
{
  "id": "adt",
  "inputFormat": "hl7v2",
  "source": {"type": "mllp", "address": ":2575"},
  "transform": {"name": "adt", "steps": [
    {"filter": {"when": "MSH.9.2 == 'A01'", "action": "accept"}},
    {"map": {"from": "PID.5.1", "to": "patient.lastName"}},
    {"map": {"from": "PID.3.repetitions.1.1", "to": "patient.ssn"}}
  ]},
  "destinations": [{"name": "ehr", "type": "http", "url": "https://ehr.example.com/inbound"}]
}
```

For `MSH|^~\&|LAB|HOSP|W|H|20260927120000||ADT^A01|MSG1|P|2.5` and
`PID|1||123^^^MRN~456^^^SSN||DOE^JOHN`, the transform sees:

```json
{
  "MSH": {"name": "MSH", "2": {"1": "^", "2": "~", "3": "\\", "4": "&"}, "3": {"1": "LAB"}, "4": {"1": "HOSP"},
          "5": {"1": "W"}, "6": {"1": "H"}, "7": {"1": "20260927120000"}, "9": {"1": "ADT", "2": "A01"},
          "10": {"1": "MSG1"}, "11": {"1": "P"}, "12": {"1": "2.5"}},
  "PID": {"name": "PID", "1": {"1": "1"},
          "3": {"1": "123", "4": "MRN", "repetitions": [{"1": "123", "4": "MRN"}, {"1": "456", "4": "SSN"}]},
          "5": {"1": "DOE", "2": "JOHN"}},
  "segments": [{"name": "MSH", "…": "…"}, {"name": "PID", "…": "…"}]
}
```

- A segment name holds that segment's **first** occurrence: `PID.5.1` is the family name. Every
  segment, in order, is in `segments` (`segments.2.5.1` is field 5, component 1 of the third
  segment), which is how you reach a second `OBX`.
- Fields use HL7 numbers. For MSH, `MSH.9` is MSH-9 (MSH-1, the field separator, is not
  included; MSH-2 holds the encoding characters).
- A field is an object of its components, even with one component: `PID.8.1`, not `PID.8`.
- A component with subcomponents (`&`) is an object of subcomponent numbers: for
  `123^^^HOSP&1.2.3&ISO`, `PID.3.4.1` is `HOSP` and `PID.3.4.2` is `1.2.3`. A component
  without subcomponents is a string (`PID.3.4` is `MRN` above), so check the messages you receive
  when a component can have both forms.
- A field that repeats (`~`) shows its first repetition, plus `repetitions` with every
  repetition in order; an empty repetition is `{}`, so positions never shift (`~456` has no
  `PID.3.1`, and `PID.3.repetitions.1.1` is `456`).
- Escape sequences (`\F\`, `\S\`, `\R\`, `\T\`, `\E\`, written with the message's escape
  character) are decoded to the message's own delimiters after the value is split, so an escaped
  delimiter is text, never a separator: `O\T\BRIEN` is `O&BRIEN`. Other escape sequences
  (`\X0D\`, `\H\`, `\.br\`, …) are kept as written. Empty fields, components, and
  subcomponents are left out, so a missing value compares equal to `''`.
- The message's own delimiters (MSH-1 and MSH-2) are used, for example `MSH#$%!@` for field `#`,
  component `$`, repetition `%`, escape `!`, and subcomponent `@`; the view is the same as for
  the standard `|^~\&`. When MSH-2 declares no subcomponent character (`MSH|^~\|`), components
  are not split into subcomponents; when it declares no escape character either (`MSH|^~|`),
  nothing is decoded. Batch headers (FHS, BHS) declare delimiters the same way. Line breaks `\n` or `\r\n` between segments are
  accepted.
- HL7 v2.1 to 2.9 are read (MSH-12, with a minor release such as `2.5.1`; surrounding spaces are
  ignored); a message without a version is read too. Any other version is refused: `400` over the API, `AR` over MLLP, with
  `unsupported HL7 version (MSH-12 must be 2.1 to 2.9)`. A `build` step with `format: hl7v2`
  must produce a supported version as well.
- The transform's output is JSON (the view above with your changes), unless it ends with a
  [`build`](#build-the-output-build) step, which can produce HL7 v2 again. A flow without
  transforms delivers the HL7 message unchanged.
- Destination transforms read the same view when the flow itself has no transform (otherwise they
  read the flow's output: JSON, or the view of what its `build` step produced).
- A message that does not start with an MSH segment is refused: `400` over the API, `AR` over
  MLLP, even when the flow has no transform. The stored original is always the message as
  received.

### Transform XML documents

Set `"inputFormat": "xml"` and the flow's transforms read each XML document as JSON:

```json
{
  "id": "orders",
  "inputFormat": "xml",
  "transform": {"name": "orders", "steps": [
    {"map": {"from": "order.@id", "to": "orderId"}},
    {"map": {"from": "order.patient.name.#text", "to": "patient"}},
    {"map": {"from": "order.item.1.@sku", "to": "secondSku"}}
  ]},
  "destinations": [{"name": "ehr", "type": "http", "url": "https://ehr.example.com/orders"}]
}
```

For this document:

```xml
<order id="42" xmlns="urn:orders" xml:lang="en">
  <patient>
    <name>DOE</name>
  </patient>
  <item sku="A"/>
  <item sku="B"/>
</order>
```

the transform sees:

```json
{"order": {"@id": "42", "@xml:lang": "en", "#ns": "urn:orders",
           "patient": {"#ns": "urn:orders", "name": {"#ns": "urn:orders", "#text": "DOE"}},
           "item": [{"#ns": "urn:orders", "@sku": "A"}, {"#ns": "urn:orders", "@sku": "B"}]}}
```

- The top-level key is the root element's name. Elements are named by their local name (without
  a namespace prefix); `#ns` holds an element's namespace URI when it has one.
- A child that appears once under its parent is an object (`order.patient`); a name that appears
  more than once is a list in document order (`order.item.0`, `order.item.1`). Check the
  documents you receive: a path such as `order.item.@sku` finds nothing when there are two
  items. The order between children of different names is not kept.
- `@name` is an attribute, written as in the document: `@id`, `@xml:lang`, `@x:id` for a prefixed
  one. Namespace declarations (`xmlns`, `xmlns:x`) are left out.
- `#text` is the element's own text with surrounding whitespace removed, so indented documents
  compare as expected (`order.patient.name.#text == 'DOE'`); it is left out when empty.
  `&amp;` and the other predefined entities are decoded. Text mixed between child elements is
  joined into the parent's `#text`.
- Any character set the document declares (`encoding="ISO-8859-1"`, `windows-1252`, …) is
  accepted, and a leading UTF-8 byte order mark is skipped.
- The transform's output is JSON, delivered as `application/json`, unless it ends with a
  [`build`](#build-the-output-build) step (for example `format: xml`). The stored original is the
  document as received.
- Only well-formed documents with one root element are accepted, with at most 256 levels of
  nesting and 100,000 elements, every namespace prefix declared, no attribute twice, the XML
  declaration (if any) first, and at most one `DOCTYPE` before the root. Anything else is refused
  (`400` over the API, rejected by a file source), even when the flow has no transform. A
  `DOCTYPE` is allowed but never processed: entities it declares are not expanded and nothing is
  fetched, so a document using one is refused.

### Transform delimited text (CSV)

Set `"inputFormat": "delimited"` and the flow's transforms read each message (a whole file, or
the body of a request) as rows:

```json
{
  "id": "roster",
  "inputFormat": "delimited",
  "delimited": {"delimiter": ";", "header": true},
  "source": {"type": "file", "dir": "/var/lib/weavster/in/roster", "pattern": "*.csv"},
  "transform": {"name": "roster", "steps": [
    {"map": {"from": "rows.0.lastName", "to": "firstPatient.lastName"}}
  ]},
  "destinations": [{"name": "ehr", "type": "http", "url": "https://ehr.example.com/roster"}]
}
```

For

```text
mrn;lastName;note
123;DOE;plain
456;ROE;"has; a delimiter"
```

the transform sees:

```json
{"header": ["mrn", "lastName", "note"],
 "rows": [{"mrn": "123", "lastName": "DOE", "note": "plain"},
          {"mrn": "456", "lastName": "ROE", "note": "has; a delimiter"}]}
```

| `delimited` field | Meaning |
|---|---|
| `delimiter` | `,` (default), `;`, `\|`, or a tab (`"\t"` in JSON). |
| `header` | `true` (default): the first row names the columns and each row is an object. `false`: each row is a list of values (`rows.0.1` is the second value of the first row), and there is no `header`. |

- Values are text. Quoting follows RFC 4180: a value in double quotes may contain the delimiter,
  line breaks, and doubled quotes (`""`). Rows may end with CRLF or LF (a CRLF inside a quoted
  value becomes LF); blank lines are skipped and a UTF-8 byte order mark is ignored.
- Header names have surrounding spaces removed (`mrn, lastName` gives `lastName`) and must be
  unique, not empty, and without dots (a dot would make the column unreachable, since paths use
  dots).
- Every row must have as many values as the first row (or the header); input with no rows at
  all, more than 100,000 rows, or more than 1,000,000 values is refused (`400`, or rejected by a
  file source), even when the flow has no transform. The reason names the problem, for example
  `a " inside a value that is not in quotes`.
- Changing `delimited` options also changes how messages still waiting for a retry are read,
  like any other change to a flow's definition.
- The whole file is one message. A transform reaches rows by position (`rows.0`, `rows.1`); there
  are no loops yet, and splitting a file into one message per row comes later.
- The transform's output is JSON, delivered as `application/json`, unless it ends with a
  [`build`](#build-the-output-build) step.

### Send HL7 v2 over MLLP

An `mllp` destination sends each message to another HL7 system over TCP, framed with MLLP, and
waits for its ACK:

```json
{
  "id": "adt-to-lab",
  "inputFormat": "hl7v2",
  "source": {"type": "mllp", "address": ":2575"},
  "destinations": [{"name": "lab", "type": "mllp", "address": "lab.example.com:2575", "timeoutMs": 10000}]
}
```

- The destination must receive an HL7 v2 message: either the message as received (the flow has
  `"inputFormat": "hl7v2"` and neither the flow nor the destination has a `transform`), or the
  output of a [`build`](#build-the-output-build) step with `"format": "hl7v2"` at the end of the
  flow's or the destination's transform. Anything else (JSON, XML) is refused when you create the
  flow.
- A message containing the MLLP end bytes (`0x1C 0x0D`) cannot be framed; its delivery fails.
- Each delivery opens its own connection, sends one frame, and waits for one framed reply within
  `timeoutMs` (default 30 seconds; replies over 1 MiB are not read).
- The delivery succeeds only on an ACK with `AA` (or `CA`) in MSA-1 whose MSA-2 is the message's
  control id (MSH-10). An `AE`/`CE` or `AR`/`CR` ACK, a reply that is not an ACK, an ACK for
  another control id, a closed connection, or no reply in time is a failed attempt: it is retried
  and then dead-lettered like any other failure. The attempt's error names the ACK code (for
  example `mllp: ACK AE (application error)`), never message content.
- A lost ACK means the message is sent again: delivery is at least once, and MLLP has no field
  for an idempotency key, so the receiver must tolerate a repeat.
- Traffic is not encrypted unless you set `tls` (below).

To send over TLS, set `"tls": true`. The destination checks the receiver's certificate and that it
is issued for the host in `address`; there is no setting to skip the check. By default the
system's trusted certificate authorities are used. For a receiver whose certificate comes from a
private CA, set `caFile` to a PEM file with that CA's certificate (for a self-signed receiver, the
certificate itself); only the certificates in `caFile` are then trusted:

```json
{"name": "lab", "type": "mllp", "address": "lab.example.com:2576", "tls": true, "caFile": "/etc/weavster/lab-ca.pem"}
```

- `caFile` must be an absolute path on the server and needs `"tls": true`. It is read for each
  message, so a replaced file is used without a restart.
- The server's `tls.minVersion` applies (TLS 1.2 by default). A receiver whose certificate cannot be verified, or a missing or
  unreadable `caFile`, fails the attempt before anything is sent; the attempt's error says why
  (for example `x509: certificate signed by unknown authority`), and it is retried like any
  other failure.
- For other framing bytes, or to send without waiting for ACKs, see
  [MLLP framing and ACK modes](#mllp-framing-and-ack-modes).
- `tls` and `caFile` apply only to mllp destinations; an http destination uses TLS with an
  `https://` URL.

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

### Processing order

Each message goes through these stages, in this order:

1. **Flow transform**: the flow's `transform` steps, one after another in the order written, on
   the message (or its [HL7 v2](#transform-hl7-v2-messages),
   [XML](#transform-xml-documents), or [delimited](#transform-delimited-text-csv) view). Each
   step sees what the steps before it produced. If a `filter` drops the message it is
   `filtered`; if a step fails (for example `"x" is not a number`) it is `errored`. Either way
   nothing is delivered. A final [`build`](#build-the-output-build) step turns the result into
   the output (HL7 v2, XML, text, or JSON); otherwise the output is the document as JSON.
2. **Destination transform**: for each destination, its own
   [`transform`](#per-destination-transforms-and-filters) steps in order, on the flow's output
   (not the original message). A `filter` here drops the message for that destination only. A
   [stopped](flow-lifecycle.md#stopping-one-destination) destination holds the message and runs
   its transform (as defined then) when it is started; a destination the message was already
   delivered to is not run again.
3. **Delivery** to each destination that kept the message and was not excluded by a
   [`destinationSet`](#route-by-content-destinationset) step in stage 1.
4. **Response transform**: if the flow has a
   [`responseSelector`](#return-a-destinations-reply) and that destination was delivered to while
   the sender waited, its `responseTransform` steps run in order on the reply, which is returned to
   the sender. Retries, and a destination that dropped or held the message, return no reply.

Step order matters. Here the `filter` reads `adult`, which the `map` step before it sets:

```json
"transform": {"steps": [
  {"map": {"from": "age.flag", "to": "adult"}},
  {"filter": {"when": "adult == 'y'", "action": "accept"}}
]}
```

Written the other way round, the filter runs before the `map`, so it sees only what the message
itself carries: a message without an `adult` field (a missing value compares equal to `''`) is
filtered, whatever `age.flag` says. A destination's filter on `adult`, by contrast, always sees
the flow's finished output, including what the `map` set.

### Build the output (`build`)

A `build` step, as the **last** step of a flow's or destination's `transform`, writes the output
from a template instead of sending the document as JSON. This is how a flow sends HL7 v2 (or XML,
or text) again. For example, receive ADT over MLLP and relay a reshaped message to a lab:

```json
{
  "id": "adt-relay",
  "inputFormat": "hl7v2",
  "source": {"type": "mllp", "address": ":2575"},
  "transform": {"steps": [
    {"set": {"field": "name", "expr": "{{PID.5.1}}^{{PID.5.2}}"}},
    {"build": {"format": "hl7v2", "template": "MSH|^~\\&|WEAVSTER|H|LAB|H|{{MSH.7.1}}||ADT^A08|{{MSH.10.1}}|P|2.5\nPID|1||{{PID.3.1}}^^^MRN||{{name}}"}}
  ]},
  "destinations": [{"name": "lab", "type": "mllp", "address": "lab.example.com:2575"}]
}
```

| `format` | Output | Content-Type |
|---|---|---|
| `json` (default) | The template must render a JSON object, and every `{{path}}` must be inside a string (`"name": "{{name}}"`), so values are always text. | `application/json` |
| `hl7v2` | The template starts with an MSH segment using the standard delimiters (a vertical bar between fields, `^~\&` in MSH-2); segments may be written on separate lines and are sent separated by CR. | `x-application/hl7-v2+er7` |
| `xml` | The template must render a well-formed XML document; `{{path}}` may appear only in element text and quoted attribute values (not in tag names, comments, CDATA sections, or declarations); an XML declaration may only say `encoding="UTF-8"`. | `application/xml` |
| `text` | Any text. | `text/plain; charset=utf-8` |

- Each `{{path}}` is replaced by that value (an empty string when missing), **escaped for the
  format**, so a value can never change the output's structure: in `hl7v2`, `| ^ ~ \ &` become
  `\F\ \S\ \R\ \E\ \T\`, and line breaks and MLLP framing bytes hex escapes such as `\X0D\` (in the example above, the
  `^` in `name` is sent as `\S\`, so the name stays one field); in `xml`, `& < > " '` become
  entities; in `json`, values are escaped as the inside of a JSON string. `text` inserts values
  as they are. To send numbers or booleans as JSON, leave out `build`: a transform's document is
  sent as JSON with its types (use `map` with `type`).
- If the result is not a valid document of its format, the step fails and the message is
  `errored`.
- A destination transform after a flow `build` reads that output's view: HL7 v2 paths after
  `format: hl7v2`, XML paths after `format: xml`. After `format: text` a destination transform
  cannot read it and is refused.
- A `responseTransform` cannot use `build`: the reply returned to the sender is JSON.

### Convert XML (instead of XSLT)

Weavster has no XSLT. The declarative equivalent is an XML flow whose transform reads the
document's [XML view](#transform-xml-documents), optionally filters it, and writes the new
document with a [`build`](#build-the-output-build) step:

```json
{
  "id": "orders-to-requests",
  "inputFormat": "xml",
  "transform": {"steps": [
    {"filter": {"when": "order.@status == 'cancelled'", "action": "reject"}},
    {"build": {"format": "xml", "template": "<request id=\"{{order.@id}}\"><patient>{{order.patient.name.#text}}</patient><first-item sku=\"{{order.item.0.@sku}}\"/><second-item sku=\"{{order.item.1.@sku}}\"/></request>"}}
  ]},
  "destinations": [{"name": "lab", "type": "http", "url": "https://lab.example.com/requests"}]
}
```

For `<order id="42" status="new" xmlns="urn:orders"><patient><name> DOE &amp; SON </name></patient><item sku="A"/><item sku="B"/></order>`
the lab receives `<request id="42"><patient>DOE &amp; SON</patient><first-item sku="A"/><second-item sku="B"/></request>`
as `application/xml`.

What XSLT does and how to do it here:

| XSLT | Here |
|---|---|
| `xsl:value-of select="/order/@id"` | `{{order.@id}}` in a `build` template (or `map`/`set` first) |
| Literal result elements | The `build` template's own markup |
| `xsl:if` / predicate that drops the message | A `filter` step |
| `xsl:choose` to pick a destination | A [`destinationSet`](#route-by-content-destinationset) step |
| Computed text (`concat(...)`) | A `set` step with `{{path}}` placeholders |

Not available: XPath functions, sorting, and loops (`xsl:for-each`, `xsl:apply-templates`). A
template addresses repeated elements by position (`order.item.0`, `order.item.1`), so it fits
documents with a known shape; lists of any length cannot be reshaped yet. Two things to check
in the documents you receive:

- An element that appears **once** is an object, not a list: with a single `<item>`,
  `order.item.0.@sku` finds nothing and `order.item.@sku` is the value. A template written for
  two items does not fit an order with one.
- A path that finds nothing is written as empty text (`sku=""`), not an error. Add a `filter`
  step to drop documents without what the template needs, for example
  `{"filter": {"when": "order.item.1.@sku", "action": "accept"}}`, and a third item is simply not
  copied.

### Write rows to a database

A `database` destination inserts each message as one row of a table in PostgreSQL (or SQLite,
for local use and tests). You choose which value of the message goes into which column:

```json
{
  "id": "lab-results",
  "inputFormat": "hl7v2",
  "transform": {"name": "results", "steps": [
    {"map": {"from": "PID.3.1", "to": "patient.mrn"}},
    {"map": {"from": "OBX.3.2", "to": "result.test"}},
    {"map": {"from": "OBX.5.1", "to": "result.value"}}
  ]},
  "destinations": [{
    "name": "warehouse", "type": "database", "driver": "postgres", "dsnEnv": "WEAVSTER_DB_WAREHOUSE",
    "table": "lab.results",
    "columns": {"mrn": "patient.mrn", "test": "result.test", "value": "result.value"},
    "keyColumn": "delivery_key"
  }]
}
```

Start the server with the connection string in the environment variable the destination names:

```bash
export WEAVSTER_DB_WAREHOUSE='postgres://weavster:secret@db.example.com:5432/warehouse?sslmode=verify-full'
weavster server --config weavster.yaml
```

and create the table yourself, for example:

```sql
CREATE TABLE lab.results (mrn text, test text, value text, delivery_key text UNIQUE);
```

| Field | Meaning |
|---|---|
| `driver` | Required. `postgres`, or `sqlite` (the connection string is then a database file path). A SQLite connection waits up to 5 seconds for a lock another program or flow holds on the file before failing with `database is locked`; a connection string that sets its own `_pragma=busy_timeout(…)` keeps it. |
| `dsnEnv` | Required. The server environment variable holding the connection string, or else the file of that name in [`secrets.dir`](server-config.md#secrets); its name must start with `WEAVSTER_DB_`. The connection string never appears in the flow. |
| `table` | Required. `table` or `schema.table`: letters, digits, and `_`. |
| `columns` | Required. Column name → path of its value in the message, such as `patient.mrn` (numbers index lists: `ids.0`). |
| `keyColumn` | Optional. A column with a unique constraint that receives the delivery's idempotency key (see below). |
| `timeoutMs` | Optional. Time allowed for one insert, 1000–120000 (default 30000). |

- The destination needs JSON: the flow's or the destination's transform output, or JSON messages
  passed through. A flow reading HL7 v2, XML, or delimited text without a transform, or ending
  with a `build` to another format, is refused when you create it.
- Values are sent as query parameters, never written into the SQL, so a quote in a value is just
  data. Each value is sent as text and the database converts it to the column's type: in
  PostgreSQL `42` into an `integer`, `5.40` into a `numeric` with every digit kept, `true` into a
  `boolean`, `2026-09-28T10:00:00Z` into a `timestamptz`, an object into `jsonb`. `null` and
  missing paths are `NULL`. A value the column cannot take fails the delivery
  (`database: a value does not fit its column's type (SQLSTATE 22P02)`). SQLite converts by the
  column's affinity (`true` stays the text `true`).
- Table and column names are checked and written in double quotes, so in PostgreSQL they are
  case-sensitive: use the names as the table has them (a table created without quotes has
  lower-case names, so `Results` does not find `results`).
- Each message is one `INSERT` statement, its own transaction, within `timeoutMs`. A failed
  insert is retried like any other delivery and then dead-lettered.
- With `keyColumn`, the insert is `ON CONFLICT (keyColumn) DO NOTHING`: a retry after a lost
  success (the row was written but the reply never arrived) inserts nothing, so the message is
  written once. The key is the same for every attempt to deliver that message to that
  destination; reprocessing a message makes a new message with a new key. Without
  `keyColumn`, a retry can insert the row twice.
- The connection string is read for every message, so a changed variable (a rotated password)
  applies without a restart: the old connections are closed and new ones opened. Connections
  are pooled per variable; a SQLite database gets one connection, so its inserts take turns. An unset variable fails the delivery:
  `database: environment variable WEAVSTER_DB_WAREHOUSE is not set, and there is no file /run/secrets/WEAVSTER_DB_WAREHOUSE`.
- Errors never quote values: PostgreSQL errors are reported by kind and SQLSTATE (for example
  `database: the table does not exist (SQLSTATE 42P01)`), and a failed connection as
  `database: could not connect (check the host, credentials, and TLS settings in the connection string)`.

### Send to another flow

A `flow` destination hands each message to another flow in the same server, as a new message of
that flow. Use it to split work into flows you can deploy, stop, and monitor on their own, for
example one flow that receives and cleans up messages and one per system that sends them on:

```json
{"id": "intake", "source": {"type": "mllp", "address": ":2575"}, "inputFormat": "hl7v2",
 "transform": {"steps": [{"map": {"from": "PID.5.1", "to": "lastName"}}]},
 "destinations": [{"name": "to-ehr", "type": "flow", "flow": "ehr-out"}]}
```

```json
{"id": "ehr-out",
 "destinations": [{"name": "ehr", "type": "http", "url": "https://ehr.example.com/inbound"}]}
```

- The target flow gets what this destination sends (the flow's output, or this destination's
  own `transform` output), read the way the target's `inputFormat` says, and processes it like
  any other message: its own transform, destinations, retries, and statistics. Its message has the
  metadata `source.flow` (the sending flow) and `source.message` (the sending message's id).
- The delivery succeeds once the target has stored its message. If the target is not started
  (stopped, paused, or only deployed), refuses the message, or fails before storing it, the attempt
  fails and is retried, then dead-lettered like any other; starting the target lets the retries
  through. The target's message also keeps the delivery's key (`source.idempotencyKey`), so a
  retry after a lost success finds it and does not store the message twice.
- What the destination sends must fit the target's `inputFormat`: JSON for a target that
  transforms JSON, HL7 v2 for `hl7v2` (the message as received, or a `build` with
  `format: hl7v2`), XML for `xml`, and `build` text for `delimited`. A flow that could not work
  is refused when you create or update either flow.
- A flow that sends to another flow depends on it, with the same rules as
  [`dependsOn`](flow-lifecycle.md): the target must exist, a flow cannot send to itself or back
  to itself through others, the target cannot be deleted while a flow sends to it, and deploying
  the sending flow deploys the target first. Starting the sending flow does not start the
  target: start it too.
- The sending flow waits while the target processes the message, so a message's time in the
  sending flow includes the target's deliveries, and stopping the sending flow waits for them.
  Keep chains short and the targets' destinations fast.
- The topology overview (`GET /api/v1/topology`) shows each route as an edge between the two
  flows.

### Route by content (`destinationSet`)

A `destinationSet` step in the flow's `transform` leaves destinations out for a message, based on
its content:

```json
"transform": {"steps": [
  {"destinationSet": {"exclude": ["archive"], "when": "kind == 'orm'"}},
  {"destinationSet": {"exclude": ["ehr", "lab"], "when": "test"}}
]},
"destinations": [
  {"name": "ehr", "type": "http", "url": "https://ehr.example.com/in"},
  {"name": "lab", "type": "http", "url": "https://lab.example.com/in"},
  {"name": "archive", "type": "file", "dir": "/var/lib/weavster/archive"}
]
```

- `when` uses the same syntax as a `filter`'s `when`; without it the destinations are always
  excluded. Steps run in order with the other steps and add up: a message can be excluded from
  several destinations by several steps.
- An excluded destination gets nothing: its own transform does not run, nothing is delivered,
  and it counts as done. A message whose every destination is excluded is `filtered`.
- Destinations can only be excluded (there is no include list: a step with `include` is refused
  with `destinationSet.include is not supported: destinations can only be excluded`), and only by the flow's
  `transform`, not by a destination's or response transform. Every name must be a destination of
  the flow; otherwise the flow is refused when you create or update it.
- The exclusion is decided once, when the message is transformed, and stored with it as the
  metadata `destinationSet.excluded` (for example `"archive"`). Retries, a restart, and starting
  a stopped destination all keep it, even if you change the flow meanwhile. Reprocessing a
  message decides again with the flow's current steps.
- If the [`responseSelector`](#return-a-destinations-reply) destination is excluded, the sender
  gets no `response` for that message (the message is still `sent` when the other destinations
  succeed).
- `weavster config validate` checks the names and placement too, so a misspelled destination is
  reported before you apply the file.

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

- The destination receives the result as `application/json`, or in its `build` step's format.
- A destination whose filter drops the message is not delivered to and counts as done. A
  [stopped](flow-lifecycle.md#stopping-one-destination) destination still holds the message
  until you start it; its filter is checked then. The
  message is `sent` once the other destinations succeed. It is `filtered` when every destination
  drops it.
- Once any destination has a `transform`, every message sent to the flow must be a JSON object
  (`400` otherwise), even when the flow itself has no `transform`; with `inputFormat: hl7v2`, an
  HL7 v2 message instead.
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
- Statistics of a removed or renamed destination remain listed, also after a restart, until you
  [reset](#5-statistics-and-events) the flow's statistics (`?lifetime=true` for the lifetime
  totals).
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
| `schedule` | Instead of `pollIntervalMs`: read the directory at cron times; see [Poll on a schedule](#poll-on-a-schedule). |
| `moveTo` | Absolute directory processed files are moved into (created if missing; a name that is already there gets the message id appended). Without it, processed files are deleted. |
| `recursive` | `true` also reads the subdirectories of `dir` (default `false`); see below. |

- The directory is read only while the flow is `started`. Stopping, pausing, or deleting the flow
  stops reading; starting it again picks up what arrived meanwhile.
- Only regular files directly in `dir` are read, unless `recursive` is `true`: symbolic links
  (to files or directories) are never followed, and hidden files (names starting with `.`, such as
  `rsync` temporary files and `.DS_Store`) are skipped. A pattern starting with `.` reads hidden
  files.
- With `"recursive": true`, files in subdirectories are read too, up to 32 levels deep, in path
  order; hidden directories are always skipped (a pattern starting with `.` reads hidden files,
  not hidden directories), and `pattern` matches the file name
  (`*.hl7` finds `2026/09/adt.hl7`). The metadata `source.file` is the path relative to `dir`
  (`2026/09/adt.hl7`), and `moveTo` (and `moveTo/rejected`) keep that path
  (`/var/lib/weavster/done/adt/2026/09/adt.hl7`). `moveTo` must then be outside `dir`, or moved
  files would be read again. No other flow may read a directory, or move files, inside a
  recursive source's `dir`; these checks also compare paths with symbolic links resolved (links
  created after the flow was saved are not detected). A subdirectory that cannot be read is logged (once) and its files are
  not read. Subdirectories are left in place when their files have been processed, and every poll
  walks the whole tree, so keep it small. A `dir` that is itself a symbolic link is followed;
  links inside it are not.
- A directory can be read by one flow only; a second flow with the same `dir` is refused (and,
  with `recursive`, any directory or `moveTo` inside it).
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
- A file the flow refuses (larger than 10 MiB, or not a JSON object when the flow has a transform,
  or not HL7 v2 with `inputFormat: hl7v2`)
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
| `username`, `passwordEnv` | Optional, together: senders must use HTTP Basic authentication with this user name (no `:`, which separates user and password in Basic authentication) and the password in the server's environment variable `passwordEnv` (or else the file of that name in [`secrets.dir`](server-config.md#secrets)). The name must start with `WEAVSTER_SOURCE_` followed by capital letters, digits, or `_`, so a flow cannot use the server's other secrets. The password never goes into the flow definition. |
| `readTimeoutMs` | Time allowed to read one request, headers and body, 1000–600000 ms; default 60000. The headers must also arrive within 10 seconds (or `readTimeoutMs`, if shorter). A sender slower than that gets its connection closed. |
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
  a message the flow refuses (for example not a JSON object when the flow has a transform, or not
  HL7 v2 with `inputFormat: hl7v2`) `400`.
  While the flow is stopping, or after it was removed, requests get `503`.
- A message stored before a later failure is still answered `202`, as the API does: the flow
  has it, and resending it would store it twice. A flow that is not running answers `503`
  (the API answers `409`), so the sender tries again later. A server processing as many messages
  as [`processing.maxConcurrent`](server-config.md#processing) allows answers `503` with
  `Retry-After: 1`, as the API does.
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

### Read rows from a database

A flow with a `database` source runs a query every few seconds while it is started and turns each
row into a message: a JSON object of the row's columns. Each row is then marked with `update`,
and the query must leave marked rows out, so it returns only rows not yet taken:

```json
{
  "id": "orders-out",
  "source": {
    "type": "database", "driver": "postgres", "dsnEnv": "WEAVSTER_DB_HIS",
    "query": "SELECT id, mrn, test_code, ordered_at FROM his.orders WHERE exported = false ORDER BY id",
    "idColumn": "id",
    "update": {"table": "his.orders", "key": "id", "set": {"exported": "true"}},
    "pollIntervalMs": 5000
  },
  "destinations": [{"name": "lab", "type": "http", "url": "https://lab.example.com/orders"}]
}
```

With the connection string in the server's environment
(`export WEAVSTER_DB_HIS='postgres://reader:secret@his.example.com/his?sslmode=verify-full'`),
each new order becomes a message such as:

```json
{"id": 42, "mrn": "A-1001", "ordered_at": "2026-09-28T10:00:00Z", "test_code": "GLU"}
```

| Field | Meaning |
|---|---|
| `driver` | Required. `postgres`, or `sqlite` (the connection string is then a database file path). A SQLite connection waits up to 5 seconds for a lock another program or flow holds on the file before failing with `database is locked`; a connection string that sets its own `_pragma=busy_timeout(…)` keeps it. |
| `dsnEnv` | Required. The server environment variable holding the connection string, `WEAVSTER_DB_…`, or else the file of that name in [`secrets.dir`](server-config.md#secrets). |
| `query` | Required. One `SELECT` (or `WITH … SELECT`) statement, without `;` inside it and without `INSERT`, `UPDATE`, `DELETE`, `MERGE`, or schema statements anywhere in it. It runs read-only: a PostgreSQL `READ ONLY` transaction, SQLite `query_only`. |
| `idColumn` | Required. The column of the result that identifies a row: the value `update` matches `key` against, kept with the message as the metadata `source.database.id`. |
| `update` | Required. `table` (or `schema.table`), `key` (the table's column holding the row id), and `set` (column → value) mark each row once its message is stored: `UPDATE table SET … WHERE key = <id>`. |
| `pollIntervalMs` | How often the query runs, 1000–3600000 (default 5000). |
| `schedule` | Instead of `pollIntervalMs`: run the query at cron times; see [Poll on a schedule](#poll-on-a-schedule). |
| `maxRows` | Rows read per poll, 1–10000 (default 100): the query runs with `LIMIT maxRows`. When a poll stops at this limit (or once the rows it holds reach 64 MiB), the next poll runs at once, so a backlog is read without waiting for the next interval or scheduled time. |
| `timeoutMs` | Time allowed for the query and for each update, 1000–120000 (default 30000). |

- Values become JSON: numbers, `true`/`false`, text, `null`; times as RFC 3339 text; bytes as
  text, or base64 when they are not UTF-8. The flow reads them with `inputFormat: json` (the
  default); other input formats are refused for a database source.
- A row is marked once its message is stored, also when processing that message then fails
  (the failure is the message's: it is retried or dead-lettered, and you can reprocess it).
  Delivery is at least once: a server that stops between storing a row and marking it reads the
  row again at the next start (look for two messages with the same `source.database.id`).
- The query must leave out the rows `update` marked (`WHERE exported = false` above); otherwise
  it returns the same rows again and they are stored again.
- `update` values are sent as text and converted to the column's type by the database; names are
  checked and quoted as for the [database destination](#write-rows-to-a-database) (case-sensitive
  in PostgreSQL). Each update is one statement, its own transaction.
- Each flow's source polls on its own, so a slow flow does not delay another's; a flow's next poll
  starts only after its previous one finished.
- A row the flow cannot store (larger than the 10 MiB message limit) is not marked, is reported
  once as a `source.database.refused` event with its id, and is skipped until the flow restarts.
- A failed poll (an unset variable, a query or update error) is logged and recorded once as a
  `source.database.failed` event with the reason, for example
  `database: environment variable WEAVSTER_DB_HIS is not set, and there is no file /run/secrets/WEAVSTER_DB_HIS`; it is retried at every interval
  (with a `schedule`, every 5 seconds until a poll succeeds).
- Give the database user only what the source needs: `SELECT` on the query's tables, and
  `UPDATE` on the marked columns.

### Poll on a schedule

File and database sources poll every `pollIntervalMs`, or, with `schedule`, at the times a cron
expression names. For example, read a drop directory every 15 minutes during office hours, and
export orders at 06:00 Berlin time:

```json
{"type": "file", "dir": "/var/lib/weavster/in/claims", "schedule": "*/15 7-19 * * MON-FRI"}
```

```json
{"type": "database", "driver": "postgres", "dsnEnv": "WEAVSTER_DB_HIS", "query": "SELECT …", "idColumn": "id",
 "update": {"table": "orders", "key": "id", "set": {"exported": "true"}},
 "schedule": "CRON_TZ=Europe/Berlin 0 6 * * *"}
```

- The expression has five fields: minute, hour, day of month, month, day of week (`*`, lists
  `1,15`, ranges `7-19`, steps `*/15`, and names `MON-FRI`, `JAN`). Descriptors work too:
  `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`, and `@every 30s` (an interval).
- Times are in the server's time zone, unless the expression starts with `CRON_TZ=Area/City `
  (an IANA zone such as `Europe/Berlin` or `America/New_York`; daylight saving is followed).
- The first poll is at the first scheduled time after the server starts watching the flow: when
  the flow is started, or when the server starts with the flow already started. Times the flow
  was stopped or the server was down are not made up. A poll that runs past the next scheduled
  time is followed by one poll, not one per missed time.
- At a scheduled time the source finishes its work before waiting for the next one: a backlog
  larger than one poll (100 files, or `maxRows` rows) is read in back-to-back polls; a file still
  being written at that moment is read once it has been unchanged for a second; and a poll that
  fails (a missing directory, a database error) is tried again every few seconds until it works.
- `schedule` and `pollIntervalMs` cannot both be set, and `schedule` applies only to file and
  database sources. An invalid expression, one that never runs (`0 0 30 2 *`), or `@every` below
  one second (use `pollIntervalMs`) is refused when you create the flow, for example
  `source.schedule "every day" is not a cron expression (…)`.

### Receive HL7 v2 over MLLP

A flow with an mllp `source` accepts HL7 v2 messages over TCP, framed with the minimal lower
layer protocol (MLLP), while it is started, and answers every message with an HL7 ACK:

```json
{
  "id": "adt",
  "source": {"type": "mllp", "address": "0.0.0.0:2575"},
  "destinations": [{"name": "archive", "type": "file", "dir": "/var/lib/weavster/out/adt"}]
}
```

| Field | Meaning |
|---|---|
| `type` | `mllp`. |
| `address` | Required. `host:port` to listen on, for example `127.0.0.1:2575`, or `:2575` for every interface. |
| `certFile`, `keyFile` | Optional, together. Absolute paths of a PEM certificate chain and its private key; the port then accepts MLLP over TLS only (see below). |

Point the sending system (an interface engine, a lab or ADT feed) at the address. Each message
is a frame: the byte `0x0B`, the HL7 message, then `0x1C 0x0D`. For example, with a small test
script:

```bash
printf '\x0bMSH|^~\\&|LAB|HOSP|WEAVSTER|HOSP|20260927120000||ADT^A01|MSG1|P|2.5\rPID|1||12345||DOE^JOHN\r\x1c\r' \
  | nc -w 2 127.0.0.1 2575 | tr '\r' '\n'
```

```text
MSH|^~\&|WEAVSTER|HOSP|LAB|HOSP|20260927120001||ACK^A01|3f9c…|P|2.5
MSA|AA|MSG1
```

- The ACK swaps the sending and receiving application and facility, carries the time it was made
  (MSH-7), `ACK^<trigger>` (MSH-9), its own control id (MSH-10), and the message's processing id
  and version; MSA-2 is the message's control id (MSH-10). The ACK always uses the standard
  delimiters `|^~\&`; values echoed from a message with other delimiters are rewritten for them
  (a literal `|` becomes `\F\`). The `source.mllp.controlId` metadata is the decoded control id.
- `AA`: the message is stored and processed like one sent with the API (same checks, transform,
  delivery, and 10 MiB limit), with the metadata `source.mllp.controlId`. Delivery problems after
  that are retried and do not change the ACK.
- `AR` (reject; sending the same message again will be rejected again): the frame is not an HL7 v2
  message (no MSH segment), is larger than 10 MiB, or the flow refuses it. A flow with a
  `transform` needs `"inputFormat": "hl7v2"` to read HL7 messages (see
  [Transform HL7 v2 messages](#transform-hl7-v2-messages)); without it the transform expects JSON
  and every HL7 message is answered `AR`. A flow without transforms passes HL7 through unchanged.
- A message the flow's filter drops is still answered `AA`: it was received and stored.
- `AE` (error; try again later): the message was not stored, because the flow was stopping,
  processing failed, or the server was busy (`server busy`; see
  [`processing`](server-config.md#processing)).
- MSA-3 says why in fixed words, never with message content.
- Several systems can be connected at once; messages on one connection are handled one after
  another, in order, each answered before the next is read. A connection that sends nothing for
  5 minutes is closed; once a message starts arriving it has 15 minutes to arrive completely.
- A message larger than 10 MiB is answered `AR` with its control id (from its MSH segment).
- The frame must start with the MSH segment (line breaks before it are allowed). Bytes sent
  outside a frame are ignored.
- The port opens and closes with the flow, like an [http source](#receive-messages-over-http): one
  flow source per port, never the server's own ports, `source.mllp.failed` events when the port
  cannot be opened, and `flow:<id>` in `weavster flow ports`. Stopping the flow or the server
  answers the message being handled, then closes the connections.
- Without `certFile` and `keyFile`, messages cross the network unencrypted. Senders are not
  authenticated either way: listen on `127.0.0.1` or a private network, or allow only the
  senders' addresses in your firewall.

To accept MLLP over TLS, give the source a certificate and key:

```json
{
  "id": "adt",
  "source": {"type": "mllp", "address": "0.0.0.0:2576", "certFile": "/etc/weavster/adt.crt", "keyFile": "/etc/weavster/adt.key"},
  "destinations": [{"name": "archive", "type": "file", "dir": "/var/lib/weavster/out/adt"}]
}
```

Test it with `openssl s_client`:

```bash
printf '\x0bMSH|^~\\&|LAB|HOSP|WEAVSTER|HOSP|20260927120000||ADT^A01|MSG1|P|2.5\rPID|1||12345||DOE^JOHN\r\x1c\r' \
  | openssl s_client -quiet -connect 127.0.0.1:2576 -CAfile /etc/weavster/adt-ca.pem | tr '\r' '\n'
```

- The port accepts TLS connections only; a sender connecting without TLS gets no ACK and is
  disconnected, as is one that does not finish the TLS handshake within 30 seconds. The server's `tls.minVersion` applies (TLS 1.2 by default).
- The certificate and key are read when the port opens; after replacing the files, stop and
  start the flow. A source whose files cannot be loaded stays closed (it never falls back to
  plain TCP) and records a `source.mllp.failed` event with the reason, as an
  [http source](#receive-messages-over-http) does. The server's own TLS key is refused.
- Client certificates (mutual TLS) are not requested.

### MLLP framing and ACK modes

Some systems frame HL7 messages with other bytes than MLLP's, or do not exchange ACKs. An mllp
source and an mllp destination both take:

| Field | Meaning |
|---|---|
| `frameStart` | Byte before each message, in hex. Default `0B` (VT). |
| `frameEnd` | One or two bytes after each message, in hex. Default `1C0D` (FS CR). |
| `ackMode` | `original` (default): the source answers every message with an HL7 ACK, and the destination waits for the receiver's ACK. `none`: the source sends nothing back, and the destination counts a message as delivered once it is written. |

For example, a feed framed with STX … ETX that expects no replies, forwarded to a lab system
that uses standard MLLP:

```json
{
  "id": "legacy-feed",
  "inputFormat": "hl7v2",
  "source": {"type": "mllp", "address": ":2577", "frameStart": "02", "frameEnd": "03", "ackMode": "none"},
  "destinations": [{"name": "lab", "type": "mllp", "address": "lab.example.com:2575"}]
}
```

- `frameStart` and the first byte of `frameEnd` must be control bytes that HL7 text never
  contains: `00`–`1F` except `09` (tab), `0A` (line feed), and `0D` (carriage return), or `7F`;
  and they must differ. The second byte of `frameEnd` can be any other byte (MLLP's is `0D`).
  Anything else is refused when you create the flow, for example
  `frameEnd starts with 0D, which can occur in a message; use a control byte such as 1C`.
- A source reads everything between the start byte and the end bytes as one message and skips
  bytes outside a frame; its ACKs use the same framing. With a one-byte `frameEnd`, a message
  cannot contain that byte.
- A message containing the destination's `frameStart` byte or `frameEnd` cannot be framed; its
  delivery fails.
- With `ackMode: none` the destination sends the message, closes its side of the connection, and
  waits up to a second for the receiver to close before counting it as delivered.
- With `ackMode: none` the sender learns nothing: on the source, a message the flow refuses or
  cannot store is dropped with no reply (look for it in the flow's messages and events); on the
  destination, a receiver that rejects the message is not noticed. Use it only for systems that
  do not send or expect ACKs.
- The fields apply only to mllp sources and destinations.

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
| `400` | The flow or one of its destinations has a `transform` (a `responseTransform` does not count) and the body is not a JSON object; the flow has `inputFormat: hl7v2`, `xml`, or `delimited` and the body is not an HL7 v2 message, a well-formed XML document, or valid delimited text; or the body could not be read. |
| `404` | Unknown flow. |
| `409` | The flow is not `started`. |
| `413` | Body larger than 10 MiB. |
| `503` | The server is busy: [`processing.maxConcurrent`](server-config.md#processing) messages are being processed and none finished within `processing.waitMs`. The reply has `Retry-After: 1`; send the message again. |

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
  "attempts":{"ehr":{"attempts":2,"lastError":"Service Unavailable","lastCode":"http:503",
    "lastAttemptAt":"2026-09-26T12:00:05Z","nextAttemptAt":"2026-09-26T12:00:09Z"}}}]
```

`attempts` has one entry per destination tried: `attempts` (how many), `lastAttemptAt` (when the
last one ended), `nextAttemptAt` (when a failed delivery is retried), and, after a failure,
`lastError` (what went wrong, in words) and `lastCode`, a code for scripts and alerts:

| `lastCode` | Meaning |
|---|---|
| `http:<status>` | The receiver answered with this status, for example `http:503`; `http:307` is a redirect that was not followed. |
| `mllp:AE`, `mllp:AR`, `mllp:CE`, `mllp:CR` | The receiver's HL7 ACK code. |
| `mllp:no-ack`, `mllp:not-an-ack`, `mllp:wrong-message`, `mllp:unknown-code`, `mllp:ack-too-large` | No reply (the connection closed), a reply that is not an ACK, an ACK for another control id, an ACK code other than AA/AE/AR/CA/CE/CR, or a reply over 1 MiB. A reply that does not come in time is `net:timeout`. |
| `sqlstate:<code>` | A PostgreSQL error, for example `sqlstate:42P01` (table does not exist) or `sqlstate:28P01` (the login was refused). |
| `net:timeout`, `net:refused`, `net:reset`, `net:dns`, `net:connect` | No answer in time, the connection refused or reset, the host name not found, or a connection that failed otherwise (an unreachable network or host). |
| `tls:certificate` | The receiver's certificate could not be verified. |
| `flow:not-running`, `flow:not-found` | A flow destination's target flow is not started, or no longer exists. |

A failure without a protocol behind it (a destination transform error, a message that cannot be
framed, a delivery cut short by a server shutdown) has no `lastCode`. A successful attempt clears
`lastError` and `lastCode`.

| Parameter | Meaning |
|---|---|
| `flowId`, `status` | Only messages of this flow / with this status. |
| `from`, `to` | Received at or after / at or before this time (RFC 3339, for example `2026-09-26T12:00:00Z`); `from` must not be after `to`. |
| `idFrom`, `idTo` | Message ids at or after / at or before these, compared byte by byte; `idFrom` must not be after `idTo`. |
| `contentType` | Only messages of this format, as their `contentType` shows (for example `hl7v2`, `json`, `raw`). |
| `minAttempts`, `maxAttempts` | Only messages where some destination took at least / at most this many attempts (1–1000). A delivered message counts its successful attempt, so `minAttempts=2` finds messages that needed a retry. |
| `metadata.KEY` | Only messages whose metadata `KEY` has exactly this value, for example `metadata.source.file=a.hl7` or `metadata.source.mllp.controlId=MSG00042`. Up to 10, each given once; all must match. |
| `limit` | Messages per page, 1–1000 (default 100). |
| `offset` | Messages to skip, for the next pages. |
| `sort` | `-receivedAt` (newest first, default), `receivedAt`, `id`, or `-id`. Other values are refused (`400`). |

The filters are applied before `limit` and `offset`, so every page holds only matching messages,
and messages received in the same instant are ordered by id, so pages neither repeat nor skip
messages while no new ones arrive.

The `X-Total-Count` response header gives how many messages match the filters in all, whatever
`limit` and `offset` are, so you can show "page 2 of 7":

```bash
curl -si -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/messages?metadata.source.file=a.hl7&minAttempts=2&limit=1' | grep -i x-total-count
```

```text
X-Total-Count: 3
```

The total is counted separately from the page, so a message that arrives or is removed in
between can make the two differ by that much. If the count fails, the page is still returned,
without `X-Total-Count`.

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
| `POST /api/v1/messages/{id}/reprocess` | `messages:send` | Sends the original content through the message's flow again. The new message (`202`, same reply as sending, also `202` with status `received` when a later step failed after it was stored) keeps the old message's metadata (except its `error`) and adds `reprocessedFrom` with the old id. The flow must be `started` (`409` otherwise). |
| `DELETE /api/v1/messages/{id}` | `messages:delete` | Removes the message (`204`). A message that is being processed or retried right now returns `409`; try again. |

A message's stored content forms are `raw` (as received) and `transformed` (after the flow
transform). What each destination was sent and what it answered are not stored as content:
the reply returned to the sender is in the send response, and each destination's outcome is in
`attempts`. Messages have no attachments.

An unknown id returns `404`. Message content can hold protected health information, so
`messages:content` is a separate permission and every search, read, and content request is
recorded in the [audit log](audit-log.md) as `phi.access`, with the messages or content part it
disclosed.

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/messages/6f1c…/content?part=raw'
```

### Remove many messages

`DELETE /api/v1/messages` (permission `messages:delete`) removes every message that matches the
[search filters](#4-find-processed-messages) (`flowId`, `status`, `from`, `to`, `idFrom`, `idTo`,
`contentType`, `minAttempts`, `maxAttempts`, `metadata.KEY`). There is no limit: every match is
removed.

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
| `restart=true` | Stops the started flows first (only the `flowId` flow, when given; other filters do not narrow which flows are stopped), removes the messages, then starts those flows again. `restarted` lists them. Stopping waits for the messages those flows are processing, so they are removed too. Paused and halted flows are left as they are. |

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

### Prune old messages

With [`prune`](server-config.md#prune) in the server configuration, the server removes old
messages every `intervalMinutes`. A pass first removes the messages received more than
`maxAgeHours` ago, then, while more than `maxMessages` are stored, the oldest ones (a few more
when several were received in the same millisecond). Only messages that are done (`sent`,
`filtered`, `errored`, `dead-lettered`) are removed; `received`, `transformed`, and `queued`
messages are never removed. They still count toward `maxMessages`, because the limit is on the
store's size: finished messages are removed first to make room, even the newest, and a store
whose excess is all unfinished messages stays above `maxMessages` until they finish. A finished message that is being worked on at that moment (for
example requeued or reprocessed) is skipped and counted once in `busy`; the next pass takes it.
Removed messages are gone for good, so [export](#export-and-import-messages) the ones you must
keep first.

| Request | Permission | What it does |
|---|---|---|
| `GET /api/v1/system/prune` | `messages:view` | The limits, whether a pass is running, the last pass, and when the next one runs. |
| `POST /api/v1/system/prune/start` | `messages:delete` | Runs a pass now, in the background (`202`). `409` when one is running, or when neither limit is set; `503` while the server is starting or shutting down. |
| `POST /api/v1/system/prune/stop` | `messages:delete` | Stops the running pass and answers once it has stopped (`200`); what it removed stays removed. `409` when no pass is running. |

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/system/prune/start
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/system/prune
```

```json
{"maxAgeHours":720,"maxMessages":0,"auditMaxAgeDays":365,"eventMaxAgeDays":90,"intervalMinutes":60,"running":false,
 "nextRunAt":"2026-09-28T13:00:00Z",
 "lastRun":{"startedAt":"2026-09-28T12:00:00Z","finishedAt":"2026-09-28T12:00:02Z",
   "removed":1250,"busy":0,"auditRemoved":310,"eventsRemoved":1200,"stopped":false}}
```

`lastRun.auditRemoved` and `lastRun.eventsRemoved` count the audit entries and stored events
removed by `prune.auditMaxAgeDays` and `prune.eventMaxAgeDays`.
`lastRun.error` says why a pass failed (for example a lost database connection), and
`lastRun.stopped` is `true` for a pass that was stopped. Every pass also records a
`messages.pruned` [event](#5-statistics-and-events) with `removed` and `busy`, and `stopped` or
`error` when they apply. Without a message store (`store.dialect: disabled`) these requests
return `503`, and a `prune` section is refused at startup.

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

With `store.dialect: postgres`, current counters and lifetime totals are kept across restarts.
They are saved with every [sample](#statistics-over-time) (`stats.sampleIntervalMs`, every minute
by default), after every reset, and when the server stops. After a crash, the counts of the last
interval before it are lost; the messages themselves are not. With `memory`, statistics start
from zero at every start.

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

The events API answers from the newest 10,000 events, kept in memory. Every event is also written
to the store in the background (within a quarter of a second), and when the server starts with
`store.dialect: postgres` it loads the newest 10,000 back: events and their ids survive a restart.
New events get larger ids than any given before the restart, so polling with `afterId` keeps
working; the ids jump at a restart (they start from the time), so do not expect them to be
consecutive. Run one server per database: two servers storing events in the same database would
give out the same ids. With `memory` the log starts empty on every start. If the store
falls behind, events are kept in the event log but not stored, and the server logs
`events not stored`. When the server stops it stores what is still queued (for at most 5 seconds). Stored events are kept until
[`prune.eventMaxAgeDays`](server-config.md#prune) removes them.

To save statistics or events to a file from the command-line client, use `dump stats "path"` or
`dump events "path"` (the newest 10,000 events).

The [topology](topology.md) (`GET /api/v1/topology`, and one flow with
`GET /api/v1/topology/flows/{flowId}`) shows each flow's, source's, and destination's `received`,
`sent`, `errored`, and `queued` counts under `activity`. Zero counts are included.

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
- With `store.dialect: postgres`, samples are stored and loaded again at start, so the series
  continues across a restart (with a gap while the server was down). With `memory` they start
  again after a restart. A lifetime reset shows as a drop to zero.
- Deleting a flow drops its samples, also from the store, so a new flow with the same `id` starts
  a new series. The server holds at most 1,000,000 samples in memory and drops the oldest past
  that; the store keeps samples for `stats.retentionHours`.
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

A message is acknowledged to its sender only after it is stored: the API and http sources answer
`202` (also when a later step then fails: the message is kept and finished), an mllp source
`AA`, a file source removes or moves the file, and a database source marks the row. If storing
fails, the sender gets an error (`500`, `AE`, or the file or row stays for the next poll) and can
send it again. With a durable store (`store.dialect: postgres`), each later step (the transformed
result, each destination's result, the final status) is written in one step with the message,
so a stop at any point leaves the message in a state the next start can finish; with `memory`
a stop loses every message.

Between storing a message and acknowledging it there is a short gap. A stop there, or a file
that cannot be removed or a row that cannot be marked, makes the sender or source deliver it
again, and it is stored a second time as a new message with its own idempotency key. So a
receiver that must not see duplicates should also check a business key (an HL7 control id, an
order number), not only `Idempotency-Key`.

Retry times are stored with the message. After a restart, the server resumes pending
retries right away. With `store.dialect: postgres`, a message that was `queued` when the server
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

- The events API shows the newest 10,000 events. Events and statistics are kept across restarts
  with PostgreSQL only.
- The first delivery attempt runs while your request waits; retries run in the background.
- Only `http`, `file`, `mllp`, and `flow` destinations are available.
- Besides this API, messages enter only through [file sources](#read-files-from-a-directory) and
  [http sources](#receive-messages-over-http), and [mllp sources](#receive-hl7-v2-over-mllp);
  database sources are not available yet.
- A `file` destination writes wherever `dir` points, with the server's permissions, and an
  `http` destination can target any address the server can reach, including internal ones.
  Only give `flows:edit` to trusted users.
