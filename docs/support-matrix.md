# Support matrix

This page lists what the `weavster` binary does **today**. It is re-checked at the end of every
delivery phase. When the README or any other page disagrees with this page, this page is correct.

Every capability sits in exactly one tier:

| Tier | Meaning |
|---|---|
| **Implemented (wired)** | Reachable from `weavster server` or the CLI and covered by an acceptance test that exercises the public API/CLI. |
| **Library-only** | Code and unit tests exist in the source tree, but the running server does not use it. You cannot reach it from the API or CLI. |
| **Enterprise-deferred** | An interface stub exists and returns a deterministic "not available" error. There is no MVP implementation. |
| **Unsupported** | No working implementation exists, in the running binary or in the source tree. |

## Server and API

Start the server with `weavster server 127.0.0.1:8080`, or with a [configuration file](server-config.md) using `weavster server --config weavster.yaml`. By default every `/api/v1` request must carry the
header `X-Weavster-CSRF: 1`. `listen.requireMarkerHeader: false` removes that requirement.

Every `/api/v1` route except login needs credentials. See [Authentication](authentication.md).

| Capability | Tier | Proof / notes |
|---|---|---|
| `GET /api/openapi.yaml` (no marker header needed) | Implemented (wired) | `TestSupportMatrixWired/openapi`. The served document omits `/api/v1/flows/{id}` and the `X-Weavster-CSRF` header, so a client generated from it is incomplete. |
| `GET /api/v1/system` status document | Implemented (wired) | `TestSupportMatrixWired/system` |
| CSRF marker enforcement (`400` without `X-Weavster-CSRF: 1`) | Implemented (wired) | `TestSupportMatrixWired/csrf-marker` |
| `TRACE`/`TRACK` rejected with `405` | Implemented (wired) | `TestSupportMatrixWired/trace-blocked` |
| `Strict-Transport-Security`, `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `X-Content-Type-Options: nosniff` headers | Implemented (wired) | `TestSupportMatrixWired/security-headers`. HSTS is sent even over plain HTTP. |
| `GET/POST /api/v1/flows`, `GET/DELETE /api/v1/flows/{id}` | Implemented (wired) | `TestSupportMatrixWired/flows-*`. Flows are saved in the configured store (see Durable flow definitions below). A new server starts with no flows. Flows are stored but **never run**. A flow `id` must be 1–128 characters from `A-Z a-z 0-9 . _ -`; anything else returns `400`. `POST` with an existing `id` returns `409`. `GET`/`DELETE` of an unknown ID returns `404`. |
| `GET /api/v1/topology`, `GET /api/v1/topology/flows/{flowId}` | Implemented (wired) | `TestSupportMatrixWired/topology-*`. The graph is built from the stored flows above. Status is whatever the flow record says. Flow nodes carry real activity counters (see below). An unknown `flowId` returns `404`. |
| `GET /api/v1/messages` search (`flowId`, `status`) | Implemented (wired) | `TestSupportMatrixWired/messages`, `TestPipelineEndToEnd`. Returns messages sent into flows with their final status. |
| Authentication (Basic or Bearer token) on every `/api/v1` route except login | Implemented (wired) | `TestAuthRequired` |
| Per-route permissions (`flows:view`, `flows:edit`, `messages:view`, `admin`) | Implemented (wired) | `TestPermissionMatrix` |
| `POST /api/v1/auth/login`, `/auth/logout`, `GET /auth/me`, `POST /auth/password` | Implemented (wired) | `TestLoginLogout`, `TestBootstrapGeneratedPassword`. Tokens are in memory and expire after 12 hours. |
| First-run `admin` account (env var, secret file, or one-time generated password with forced change) | Implemented (wired) | `TestBootstrapGeneratedPassword`, `TestBootstrapPasswordSources` |
| Password policy and lockout (`auth.*` config) | Implemented (wired) | `TestBootstrapPasswordSources` |
| Durable local users (password changes and lockout state included) | Implemented (wired) | `TestUsersSurviveRestart`. Durable with `store.dialect: sqlite`; the first-run `admin` is created once per database. |
| User administration API | Unsupported | `admin` is the only account. |
| Audit of API calls (mutations, message reads, logins, failed credentials) with case-insensitive redaction | Implemented (wired) | `TestAuditLog`. Written to stderr only; not stored or searchable. See [Audit log](audit-log.md). |
| HTTPS listener (`listen.tlsAddress`, `tls.certFile/keyFile/minVersion`) | Implemented (wired) | `TestServerConfigTLS` |
| mTLS | Unsupported | |
| Message store selection: `memory` (default), `sqlite`, `disabled` | Implemented (wired) | `TestServerConfigStore`. SQLite creates `<dataDir>/weavster.db` and runs migrations at startup. `disabled` makes message search return `503` and message intake unavailable. |
| PostgreSQL message store | Unsupported | `store.dialect: postgres` is accepted, but startup fails after the retries because the schema uses SQLite-only SQL. |
| Store connection retries (`store.maxRetry`, `store.retryWaitMs`) | Implemented (wired) | `TestServerConfigErrors/retry-exhausted`, `TestServerStopDuringStoreRetry`. PostgreSQL only. |
| Durable flow definitions | Implemented (wired) | `TestFlowsSurviveRestart`. Durable with `store.dialect: sqlite`. With `memory` or `disabled`, flows are lost on restart. |
| Server configuration file (`weavster server --config FILE`) | Implemented (wired) | `TestServerConfigListen`, `TestServerConfigErrors`. Strict YAML: unknown keys and invalid values exit `1`. See [Server configuration](server-config.md). |
| Refuse to run as root (override: `WEAVSTER_ALLOW_ROOT=1`) | Implemented (wired) | `TestSupportMatrixPrivilegedGuard` |
| `/metrics` (Prometheus), OpenTelemetry | Library-only | Not mounted or initialized by the server. |
| Web UI | Unsupported | Only the JSON topology API exists. |

## Message processing

| Capability | Tier | Notes |
|---|---|---|
| Message intake: `POST /api/v1/flows/{id}/messages` | Implemented (wired) | `TestPipelineEndToEnd`. See [Processing messages](processing-messages.md). |
| Receive → persist → filter → transform → deliver to each destination, with per-destination results and aggregate status | Implemented (wired) | `TestPipelineEndToEnd`. Synchronous, one attempt; no response processing. |
| `http` and `file` destinations | Implemented (wired) | `TestPipelineEndToEnd` |
| Other sources and destinations (file/HTTP/TCP-MLLP listeners, database, SMTP, SOAP/REST web service, document, in-process inter-flow) | Library-only | Flows do not listen on their own ports or poll anything. |
| Per-flow and per-destination statistics (`GET /api/v1/flows/{id}/stats`) | Implemented (wired) | `TestStatsEventsTopology`. In memory; reset, dump, and time series are not available. |
| Event log (`GET /api/v1/events`, `type`/`flowId`/`limit`) with processing events | Implemented (wired) | `TestStatsEventsTopology`. In memory, newest 10,000; no count, export, or max-ID operations. |
| Topology flow-node `activity` from real counters, zeros included | Implemented (wired) | `TestStatsEventsTopology`. Edge activity is not reported. |
| YAML DSL `map`, `set`, `filter` steps | Implemented (wired) | `TestPipelineEndToEnd`. `build` and `destinationSet` are not supported. |
| WASM executor (wazero), module registry | Library-only | Not used by the server. The executor has no WASI host. |
| Scheduler (durable jobs, leases, interval/cron) | Library-only | |
| Retries, backoff, dead-letter | Unsupported | A failed delivery leaves the message `queued`; nothing retries it. |
| Alerts and SMTP/webhook notifiers | Library-only | |
| Config-as-code (validate/plan/apply/drift), Git store | Library-only | No CLI command or API endpoint exposes them. |
| Legacy import | Unsupported | `internal/migrate` reads a made-up `<weavster-export>` XML schema, not any real legacy export format. |

## CLI

| Capability | Tier | Proof / notes |
|---|---|---|
| `weavster server [--config FILE] [address]` | Implemented (wired) | `TestSupportMatrixCLI/server-subcommand`, `TestServerConfigListen` |
| `weavster` with no subcommand | Implemented (wired) | `TestSupportMatrixCLI/no-subcommand`. Starts the server on `127.0.0.1:8080`, **not** an interactive shell. |
| `weavster -h` | Implemented (wired) | `TestRunHelpAndVersion` |
| `weavster -v` | Implemented (wired) | `TestRunHelpAndVersion`. Prints the **local binary** version, not the server's. |
| `weavster -s script.txt` batch mode | Implemented (wired) | `TestSupportMatrixCLI/batch-*`. Commands: `help`, `status`, `version`, `flow list`, `user list`, `quit`/`exit`. `user list` always prints nothing. Exits `2` if any line is an unknown command, cannot reach the server, or `flow list` gets an unreadable reply. **`status` exits `0` even when the server answers with an HTTP error**; it prints the error body. |
| `-a address` | Implemented (wired) | `TestSupportMatrixCLI/batch-*`. Used only by `-s`. Include the scheme, for example `http://127.0.0.1:8080` (default). |
| `-u`, `-p` | Implemented (wired) | `TestCLICredentials`. Sent as HTTP Basic credentials by `-s` batch mode. |
| `-c connection-file` | Unsupported | Parsed and ignored. A missing file is not an error. |
| `-d` | Unsupported | Parsed, but error output is identical with or without it. |
| Interactive shell | Unsupported | |
| `weavster test [--filter NAME] [--format junit\|json] [--output DIR]` | Implemented (wired) | `TestRunTestCommand`. Runs four built-in codec round-trip fixtures (`identity/hl7`, `identity/json`, `identity/xml`, `identity/raw`). It does not discover your flows or fixtures. |

Example:

```bash
cat > smoke.txt <<'EOF'
status
flow list
EOF
weavster -a http://127.0.0.1:8080 -u admin -p 'PASSWORD' -s smoke.txt
```

Expected output: the `/api/v1/system` JSON document, followed by the name of each flow.

## Build and packaging

| Capability | Tier | Proof / notes |
|---|---|---|
| Static `CGO_ENABLED=0` builds for `linux/amd64`, `linux/arm64`, `darwin/arm64` | Implemented (wired) | CI `cross-build` job. No release binaries are published yet. |
| Distroless non-root container image | Implemented (wired) | CI `docker` job. Build it yourself with `docker build -t weavster .`. No image is published. |
| Signed releases, `curl \| bash` installer | Unsupported | |
| Terraform / Pulumi samples (`iac/`) | Library-only | The binary does not read any value the samples emit. |

## Enterprise-deferred stubs

Only these stubs exist in the source tree. None of them can be selected from configuration yet.

| Stub | Behavior |
|---|---|
| Broker queue/topic source and sink | Returns `adapters: enterprise adapter not implemented in MVP` |
| DICOM source and sink | Returns `adapters: enterprise adapter not implemented in MVP` |
| DICOM codec | Returns `codec: enterprise feature requires a licensed library` |
| KMS/Vault key rotation | Returns `secrets: enterprise feature not available` |

There are **no** OIDC/SAML, OPA/Cedar, SIEM, Kubernetes, Redis/NATS, or object-storage stubs.
Those are Enterprise items with no code in the source tree.

## Codecs

Codecs are library-only: the server never parses a message. You can exercise them with
`weavster test`, which covers HL7 v2, JSON, XML, and raw.

<!-- codec-table: every cell is checked against codecs.CoverageMatrix() by TestSupportMatrixCodecs -->
| Codec | Tier | Versions / segments | Acknowledgment | Notes |
|---|---|---|---|---|
| `delimited` | Library-only | any (configurable delimiter) | no | tab/pipe/comma; optional header |
| `hl7v2` | Library-only | 2.x segment/field/component/repetition | yes | MSH/MSA ACK |
| `json` | Library-only | RFC 8259 | no | stdlib encoding/json |
| `xml` | Library-only | XML 1.0 (XXE-safe) | no | no DTD/external-entity resolution by construction |
| `x12` | Library-only | ISA/GS/ST envelope | yes | 997 functional acknowledgment |
| `ncpdp` | Library-only | Telecommunication (FS/GS/RS delimiters) | no | fixed-width amount formatting; response limited |
| `raw` | Library-only | any binary | no | passthrough |
| `dicom` | Enterprise-deferred |  | no | requires a licensed library; interface stub only (gap #12) |

## Delivery guarantees per adapter

The server makes **one** delivery attempt per destination; a failure leaves the message
`queued` and is not retried. So today every wired destination is at-most-once. Rows marked
"library" describe adapters the server does not use yet. The outbox library's
`SemanticsForAdapter` labels non-TCP sinks "exactly-once", but that label is not yet true.

| Adapter | Source | Sink | Guarantee | Sends idempotency key |
|---|---|---|---|---|
| File | library | wired | at-most-once (one attempt) | no |
| HTTP | library | wired | at-most-once (one attempt) | yes: `Idempotency-Key` header, the same for every attempt to deliver a message to a destination |
| TCP/MLLP | library | library | not wired | no (the protocol has no field for one) |
| Database | library | library | not wired | no |
| SMTP | — | library | not wired | no |
| Web service (SOAP/REST) | — | library | not wired | no |
| Document | — | library | not wired | no |
| In-process inter-flow | library | library | not wired | no |
| Broker, DICOM | Enterprise-deferred | Enterprise-deferred | — | — |
