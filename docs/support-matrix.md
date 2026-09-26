# Support matrix

This page lists what the `weavster` binary does **today**. It is re-checked at the end of every
delivery phase. When the README or any other page disagrees with this page, this page is correct.

Every capability sits in exactly one tier:

| Tier | Meaning |
|---|---|
| **Implemented (wired)** | Reachable from `weavster server` or the CLI and covered by an acceptance test that exercises the public API/CLI. |
| **Library-only** | Code and unit tests exist in the source tree, but the running server does not use it. You cannot reach it from the API or CLI. |
| **Enterprise-deferred** | An interface stub exists and returns a deterministic "not available" error. There is no MVP implementation. |
| **Unsupported** | Neither the running binary nor the source tree provides it. |

## Server and API

Start the server with `weavster server 127.0.0.1:8080`. It serves plain HTTP only. Every
`/api/v1` request must carry the header `X-Weavster-CSRF: 1`.

| Capability | Tier | Proof / notes |
|---|---|---|
| `GET /api/openapi.yaml` (no marker header needed) | Implemented (wired) | `TestSupportMatrixWired/openapi` |
| `GET /api/v1/system` status document | Implemented (wired) | `TestSupportMatrixWired/system` |
| CSRF marker enforcement (`400` without `X-Weavster-CSRF: 1`) | Implemented (wired) | `TestSupportMatrixWired/csrf-marker` |
| `TRACE`/`TRACK` rejected with `405` | Implemented (wired) | `TestSupportMatrixWired/trace-blocked` |
| `X-Frame-Options`, `Content-Security-Policy: frame-ancestors 'none'`, `nosniff` headers | Implemented (wired) | `TestSupportMatrixWired/security-headers` |
| `GET/POST /api/v1/flows`, `GET/DELETE /api/v1/flows/{id}` | Implemented (wired) | `TestSupportMatrixWired/flows-*`. Flows live **in memory** and are lost on restart. The server starts with one sample flow, `admit`. Flows are stored but **never run**. |
| `GET /api/v1/topology`, `GET /api/v1/topology/flows/{flowId}` | Implemented (wired) | `TestSupportMatrixWired/topology-*`. The graph is built from the in-memory flows above. Status is whatever the flow record says, and activity counters are always zero. |
| `GET /api/v1/messages` search | Implemented (wired) | `TestSupportMatrixWired/messages`. Always returns `[]`, because nothing writes messages yet. |
| Authentication / login | Unsupported | No API route checks credentials. Every `/api/v1` route, including `POST` and `DELETE`, is **unauthenticated**. No login endpoint exists. |
| Authorization and audit of API calls | Library-only | The `auth` and `audit` packages exist, but request handlers never call them. |
| Built-in `admin` user | Unsupported | The server attempts to seed one, but the password fails its own policy, so no user exists. |
| `Strict-Transport-Security` header | Implemented (wired) | Sent on every response, even over plain HTTP. |
| HTTPS / TLS listener | Library-only | `gateway.TLSOptions` exists, but the server only listens on plain HTTP. |
| mTLS | Unsupported | |
| Persistent storage (SQLite / PostgreSQL) | Library-only | The server uses an in-memory store. The PostgreSQL backend cannot start (SQLite-only SQL). |
| Server configuration file | Unsupported | The only setting is the listen address (positional argument, default `127.0.0.1:8080`). |
| Refuse to run as root (override: `WEAVSTER_ALLOW_ROOT=1`) | Implemented (wired) | `TestPrivilegedGuard` |
| `/metrics` (Prometheus), OpenTelemetry | Library-only | Not mounted or initialized by the server. |
| Web UI | Unsupported | Only the JSON topology API exists. |

## Message processing

| Capability | Tier | Notes |
|---|---|---|
| Sources and destinations (file, HTTP, TCP/MLLP, database, SMTP, SOAP/REST web service, document, in-process inter-flow) | Library-only | Nothing in the server starts an adapter or receives a message. |
| Receive → filter → transform → route → deliver pipeline | Unsupported | |
| YAML DSL transforms | Library-only | The compiler validates DSL, but its generated code discards the steps and returns the input unchanged. |
| WASM executor (wazero), module registry | Library-only | Not used by the server. The executor has no WASI host. |
| Scheduler (durable jobs, leases, interval/cron) | Library-only | |
| Outbox, retries, dead-letter | Library-only | |
| Alerts and SMTP/webhook notifiers | Library-only | |
| Config-as-code (validate/plan/apply/drift), Git store | Library-only | No CLI command or API endpoint exposes them. |
| Legacy import | Unsupported | `internal/migrate` reads a toy XML schema, not a real legacy export format. |

## CLI

| Capability | Tier | Proof / notes |
|---|---|---|
| `weavster server [address]` | Implemented (wired) | `TestRunServerSubcommand` |
| `weavster` with no subcommand | Implemented (wired) | Starts the server, **not** an interactive shell. |
| `weavster -h` | Implemented (wired) | `TestRunHelpAndVersion` |
| `weavster -v` | Implemented (wired) | Prints the **local binary** version, not the server's. |
| `weavster -s script.txt` batch mode | Implemented (wired) | `TestShellBatchMode`. Commands: `help`, `status`, `version`, `flow list`, `user list`, `quit`/`exit`. `user list` always prints nothing. Exit code `2` on any failed command. |
| `-a address` | Implemented (wired) | Used only by `-s`. The address must include the scheme, for example `http://127.0.0.1:8080`. |
| `-u`, `-p` | Unsupported | Parsed but never sent to the server. |
| `-c connection-file` | Unsupported | Parsed and ignored. A missing file is not an error. |
| `-d` | Implemented (wired) | Adds error detail to `-s` failures. |
| Interactive shell | Unsupported | |
| `weavster test [--filter NAME] [--format junit\|json] [--output DIR]` | Implemented (wired) | `TestRunTestCommand`. Runs four built-in codec round-trip fixtures (`identity/hl7`, `identity/json`, `identity/xml`, `identity/raw`). It does not discover your flows or fixtures. |

Example:

```bash
cat > smoke.txt <<'EOF'
status
flow list
EOF
weavster -a http://127.0.0.1:8080 -s smoke.txt
```

Expected output: the `/api/v1/system` JSON document, followed by `Patient Admit`.

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

<!-- codec-table: kept in sync with codecs.CoverageMatrix() by TestSupportMatrixCodecs -->
| Codec | Tier | Versions / segments | Acknowledgment | Notes |
|---|---|---|---|---|
| `delimited` | Library-only | any (configurable delimiter) | — | tab/pipe/comma; optional header |
| `hl7v2` | Library-only | 2.x segment/field/component/repetition | MSH/MSA ACK | |
| `json` | Library-only | RFC 8259 | — | |
| `xml` | Library-only | XML 1.0 | — | No DTD or external-entity resolution |
| `x12` | Library-only | ISA/GS/ST envelope | 997 | |
| `ncpdp` | Library-only | Telecommunication (FS/GS/RS delimiters) | — | Fixed-width amount formatting; response handling limited |
| `raw` | Library-only | any binary | — | Passthrough |
| `dicom` | Enterprise-deferred | — | — | Stub only |

## Delivery guarantees per adapter

**The running server delivers nothing**, so it makes no delivery guarantee. The table below
describes the library adapters for when they are wired in later phases. No sink sends an
idempotency key today, so no destination is exactly-once. The outbox library's
`SemanticsForAdapter` labels non-TCP sinks "exactly-once", but that label is not yet true.

| Adapter | Source | Sink | Guarantee when retried through the outbox | Sends idempotency key |
|---|---|---|---|---|
| File | yes | yes | at-least-once | no |
| HTTP | yes | yes | at-least-once | no |
| TCP/MLLP | yes | yes | at-least-once | no |
| Database | yes | yes | at-least-once | no |
| SMTP | — | yes | at-least-once | no |
| Web service (SOAP/REST) | — | yes | at-least-once | no |
| Document | — | yes | at-least-once | no |
| In-process inter-flow | yes | yes | at-least-once | no |
| Broker, DICOM | Enterprise-deferred | Enterprise-deferred | — | — |
