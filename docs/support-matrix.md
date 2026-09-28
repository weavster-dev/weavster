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
| `GET /api/openapi.yaml` (no marker header needed) | Implemented (wired) | `TestSupportMatrixWired/openapi`, `TestOpenAPIPublished`, `TestOpenAPIContract`. The served document is exactly `agent-docs/openapi.yaml`; every route is documented and every documented operation is contract-tested. |
| Unversioned API paths (`/api/flows` = latest, today `/api/v1/flows`); `Weavster-API-Version` reply header | Implemented (wired) | `TestUnversionedAPI`. See [REST API](api.md#versions). |
| System information: `GET /api/v1/system` (per request, TLS as configured), `/about`, `/password-requirements`, `/resources`, `/guid` | Implemented (wired) | `TestSupportMatrixWired/system`, `TestSystemInfo`. See [System information](system-info.md). |
| CSRF marker enforcement (`400` without `X-Weavster-CSRF: 1`) | Implemented (wired) | `TestSupportMatrixWired/csrf-marker` |
| `TRACE`/`TRACK` rejected with `405` | Implemented (wired) | `TestSupportMatrixWired/trace-blocked` |
| `Strict-Transport-Security`, `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `X-Content-Type-Options: nosniff` headers | Implemented (wired) | `TestSupportMatrixWired/security-headers`. HSTS is sent even over plain HTTP. |
| `GET/POST /api/v1/flows`, `GET/PUT/DELETE /api/v1/flows/{id}` | Implemented (wired) | `TestSupportMatrixWired/flows-*`, `TestFlowUpdateAndEnable`. Flows are saved in the configured store (see Durable flow definitions below). A new server starts with no flows. A flow processes messages once it is `started` (see Flow lifecycle below). A flow `id` must be 1–128 characters from `A-Z a-z 0-9 . _ -` and not a reserved route word (`export`, `import`, `redeploy-all`, `connector-names`, `ports-in-use`, `stats`, or `<action>-all`); anything else returns `400`. `POST` with an existing `id` returns `409`. `GET`/`DELETE` of an unknown ID returns `404`. |
| `GET /api/v1/topology`, `GET /api/v1/topology/flows/{flowId}` | Implemented (wired) | `TestSupportMatrixWired/topology-*`. The graph is built from the stored flows above. Status is whatever the flow record says. Flow nodes carry real activity counters (see below). An unknown `flowId` returns `404`. |
| `GET /api/v1/messages` search (`flowId`, `status`, `from`/`to`, `limit`/`offset`, `sort`), `GET /api/v1/messages/{id}`, `/content`, `DELETE`, `POST …/reprocess` | Implemented (wired) | `TestSupportMatrixWired/messages`, `TestPipelineEndToEnd`, `TestMessagesAPI`. Filters run in the store before paging. Content needs `messages:content`; search, read, and content are audited. Export/import (`GET /api/v1/messages/export`, `POST /api/v1/messages/import`, gzip, optional AES-256-GCM, `TestMessageArchive`; CLI `exportmessages`/`importmessages`). See [Processing messages](processing-messages.md#4-find-processed-messages). |
| Authentication (Basic or Bearer token) on every `/api/v1` route except login | Implemented (wired) | `TestAuthRequired` |
| Per-route permissions (`flows:view`, `flows:edit`, `messages:view`, `admin`) | Implemented (wired) | `TestPermissionMatrix` |
| `POST /api/v1/auth/login`, `/auth/logout`, `GET /auth/me`, `POST /auth/password` | Implemented (wired) | `TestLoginLogout`, `TestBootstrapGeneratedPassword`. Tokens are in memory and expire after 12 hours. |
| First-run `admin` account (env var, secret file, or one-time generated password with forced change) | Implemented (wired) | `TestBootstrapGeneratedPassword`, `TestBootstrapPasswordSources` |
| Password policy and lockout (`auth.*` config) | Implemented (wired) | `TestBootstrapPasswordSources` |
| Durable local users (password changes and lockout state included) | Implemented (wired) | `TestUsersSurviveRestart`. Durable with `store.dialect: sqlite`; the first-run `admin` is created once per database. |
| User administration (`/api/v1/users`, CLI `user list/add/remove/changepw`) | Implemented (wired) | `TestUserAdministration`, `TestPermissionMatrix`. Permission `users:admin`. See [Authentication](authentication.md#manage-users). |
| Config map, global scripts, settings (`/api/v1/configmap`, `/scripts`, `/settings`; CLI `importmap`, `exportmap`, `importscripts`, `exportscripts`) | Implemented (wired) | `TestConfigItems`. Stored and managed only: flows do not use the config map and scripts are not run yet. See [Config map, scripts, and settings](config-items.md). |
| Code snippets and snippet libraries (`/api/v1/snippets`, `/api/v1/snippet-libraries`; CLI `snippet [library]` with `list`, `import`, `export`, `remove`) | Implemented (wired) | `TestSnippets`. Stored and managed only: flows do not run snippets yet. See [Code snippets and libraries](snippets.md). |
| Bulk message removal (`DELETE /api/v1/messages` by `flowId`/`status`/`from`/`to`, `all=true`, `restart=true`); CLI `clearallmessages`, `dump stats`, `dump events` | Implemented (wired) | `TestBulkMessageRemoval`. Messages being processed are kept and counted as `busy`. See [Processing messages](processing-messages.md#remove-many-messages). |
| Message trends (`GET /api/v1/messages/trends`: hourly or daily counts by status, optional flow) | Implemented (wired) | `TestMessageTrends` (SQLite and memory). Counted from stored messages. See [Processing messages](processing-messages.md#message-trends). |
| XML responses by content negotiation (`Accept: application/xml`); request bodies JSON only | Implemented (wired) | `TestXMLResponses`, `TestJSONToXML`. See [JSON and XML](api-formats.md). |
| Statistics over time (`GET /api/v1/stats/series`: sampled lifetime statistics per flow; server config `stats.sampleIntervalMs`, `stats.retentionHours`) | Implemented (wired) | `TestStatsSeries`. Samples are held in memory and restart empty. See [Processing messages](processing-messages.md#statistics-over-time). |
| CI/CD samples: GitHub Actions and GitLab CI, plan on pull/merge request and apply on merge to `main` (`docs/examples/ci/`) | Implemented (samples) | `TestCISamples` runs each sample's `weavster` commands against a server and checks the page shows the files. Install is `go install` until release binaries exist. See [CI/CD](ci-cd.md). |
| Alert definitions (`/api/v1/alerts`: create, get, update, delete, enable/disable, import with `force`, options); CLI `importalert`, `exportalert` | Implemented (wired) | `TestAlerts`. Definitions are stored and validated only: alerts do not fire yet. See [Alerts](alerts.md). |
| Dynamic lookups (`/api/v1/lookups`: groups, prefix matching, get, exists, batch, put, delete, import with `replace`) | Implemented (wired) | `TestLookups` (SQLite durability, permissions). Stored and served only: flows do not read lookups yet. See [Dynamic lookups](lookups.md). |
| Full configuration export/import (`GET /api/v1/config/export`, `POST /api/v1/config/import` with `force`, `nodeploy`, `overwriteConfigMap`); CLI `exportcfg`, `importcfg` | Implemented (wired) | `TestConfigTransfer`. Users are not included. See [Export and import the whole configuration](config-transfer.md). |
| Config-as-code document: typed sections for flows, alerts, snippets, libraries, scripts, config map, settings; strict validation; JSON Schemas per artifact kind; `POST /api/v1/config/validate`, CLI `config validate` and `weavster config validate FILE...` (offline: no server, no database) | Implemented (wired) | `TestConfigValidate`, `TestConfigValidateOffline`, `TestSchemasPublished`. See [Config-as-code documents](config-as-code.md). |
| Config-as-code plan and diff against the live server (`POST /api/v1/config/plan`; CLI `config diff`, `config plan`) | Implemented (wired) | `TestConfigPlan`, `TestLivePlan`. Side-effect free; sections left out of the document are not managed. See [Config-as-code documents](config-as-code.md#see-what-would-change). |
| Config-as-code apply (`POST /api/v1/config/apply` with the plan fingerprint, `dryRun`, `reason`; CLI `config apply`) | Implemented (wired) | `TestConfigApply`, `TestConfigApplyHandler`. Stale plans refused; a failing change rolls back every applied change; each attempt audited with its result. See [Apply](config-as-code.md#apply). |
| Events: search by type, flow, time, and `afterId`; get one, count, max id, export (`/api/v1/events…`) | Implemented (wired) | `TestEventsAPI`, `TestEventHandlers`. In memory: the newest 10,000 events, lost on restart. See [Processing messages](processing-messages.md#5-statistics-and-events). |
| Exit codes (shell, `server`, `test`) and `Error:` format; deprecated `channel`/`codetemplate` command names | Implemented (wired) | `TestExitCodesAndDeprecatedCommands`, `TestServerConfigErrors`. See [Exit codes and errors](cli.md#exit-codes-and-errors). |
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
| Per-destination start/stop (`POST /api/v1/flows/{id}/destinations/{name}/{start,stop}`) | Implemented (wired) | `TestDestinationStartStop`. Sources are not separate connectors yet. |
| Per-destination transforms and filters (destination `transform`) | Implemented (wired) | `TestDestinationTransforms`. Same DSL steps as the flow transform, applied to the flow output per destination. See [Processing messages](processing-messages.md#per-destination-transforms-and-filters). |
| Response transform and response selector (`responseTransform`, `responseSelector`) | Implemented (wired) | `TestResponseSelector`. The selected destination's reply to the first delivery attempt is returned by `POST /api/v1/flows/{id}/messages`; only `http` destinations reply. See [Processing messages](processing-messages.md#return-a-destinations-reply). |
| Bulk update (`PUT /api/v1/flows`), connector names (`GET /api/v1/flows/connector-names`), ports in use (`GET /api/v1/flows/ports-in-use`), `initialState` for startup auto-deploy | Implemented (wired) | `TestFlowOperations`. API only. Ports in use lists the server's API listeners; flows do not listen on ports yet. See [Processing messages](processing-messages.md#update-several-flows-at-once) and [Flow lifecycle](flow-lifecycle.md#initial-state). |
| Flow definitions validated against the published `agent-docs/schemas/flow.schema.json` (create, update, import; unknown fields rejected) | Implemented (wired) | `TestFlowSchemaEnforced`, `TestFlowSchemaPublished` |
| Flow dependencies (`dependsOn`, validated, delete-protected), export (`GET /api/v1/flows/export`), import (`POST /api/v1/flows/import`, `overwrite`) | Implemented (wired) | `TestFlowExportImport`, `TestDeployDependenciesAndDeleteRunning`. Deploy also deploys undeployed dependencies; deleting a running flow undeploys it first. See [Flow dependencies, export, and import](flow-export-import.md). |
| Flow update/rename (`PUT /api/v1/flows/{id}`), enable/disable, auto-deploy of enabled flows at startup (`flows.deployOnStartup`) | Implemented (wired) | `TestFlowUpdateAndEnable`, `TestDeployOnStartupDisabled` |
| Flow lifecycle: deploy, undeploy, start, stop, pause, halt, resume, redeploy-all, with transitions enforced | Implemented (wired) | `TestFlowLifecycle`. API only. Deploy also deploys undeployed dependencies. See [Flow lifecycle](flow-lifecycle.md). |
| All-flows actions (`POST /api/v1/flows/{deploy,undeploy,start,stop,pause,halt,resume}-all`), statistics for all flows (`GET /api/v1/flows/stats`), statistics reset (`POST /api/v1/flows/stats/reset`, `POST /api/v1/flows/{id}/stats/reset`, `lifetime`) | Implemented (wired) | `TestAllFlowsLifecycleAndStats`, `TestFlowCLI`. CLI: `resetstats [lifetime]`, `flow reset-stats`. See [Flow lifecycle](flow-lifecycle.md#act-on-all-flows-at-once). |
| Message intake: `POST /api/v1/flows/{id}/messages` | Implemented (wired) | `TestPipelineEndToEnd`. See [Processing messages](processing-messages.md). |
| Receive → persist → filter → transform → deliver to each destination, with per-destination results and aggregate status | Implemented (wired) | `TestPipelineEndToEnd`. Synchronous, one attempt; response processing through `responseSelector`/`responseTransform` (below). |
| `http` and `file` destinations | Implemented (wired) | `TestPipelineEndToEnd`, `TestHTTPDestinationOptions`. `http` takes `method` (POST/PUT/PATCH), `timeoutMs`, and `maxRedirects` (default 0; only 307/308, never https to http). |
| `mllp` destination: sends HL7 v2 over TCP (`address`, `timeoutMs`) and succeeds only on an `AA`/`CA` ACK for the message's control id | Implemented (wired) | `TestMLLPDestination`, `TestMLLPSinkACK`. Needs an HL7 v2 message: HL7 passthrough (`inputFormat: hl7v2`, no transforms) or a `build` step with `format: hl7v2`; a connection per delivery; no TLS yet. See [Send HL7 v2 over MLLP](processing-messages.md#send-hl7-v2-over-mllp). |
| File source: a flow polls a directory (`source: {type: file, dir, pattern, pollIntervalMs, moveTo}`); files deleted or moved after the message is stored; rejected files moved aside or skipped | Implemented (wired) | `TestFileSource`, `TestCheckSource`. At-least-once; only while the flow is started; regular files directly in `dir`, not symlinks or subdirectories. See [Read files from a directory](processing-messages.md#read-files-from-a-directory). |
| HTTP source: a flow listens on its own address (`source: {type: http, address, path, method}`) while it is started | Implemented (wired) | `TestHTTPSource`, `TestHTTPSourceSecured`, `TestSourceHandler`, `TestSourceHandlerBasicAuth`, `TestCheckSource`. Optional HTTP Basic (`username`, `passwordEnv`), HTTPS (`certFile`, `keyFile`), and `readTimeoutMs` (`TestHTTPSourceReadTimeout`); one flow source per port; listed by `GET /api/v1/flows/ports-in-use`. See [Receive messages over HTTP](processing-messages.md#receive-messages-over-http). |
| `destinationSet` step: the flow transform excludes destinations per message (`exclude`, optional `when`; exclusion only, D-20); stored with the message as `destinationSet.excluded` | Implemented (wired) | `TestDestinationSet`, `TestProcessDestinationSet`, `TestCheckTransforms`, `TestValidateRefuses`. See [Route by content](processing-messages.md#route-by-content-destinationset). |
| Processing order: flow transform steps in order → each destination's steps on the flow output → delivery → response transform | Implemented (wired) | `TestProcessingOrder`, `TestDestinationTransforms`. See [Processing order](processing-messages.md#processing-order). |
| `build` step: the last step of a flow or destination transform renders the output from a template as JSON, HL7 v2, XML, or text, with values escaped for the format and the result checked | Implemented (wired) | `TestBuildOutput`, `TestProcessBuild`, `TestBuild`. See [Build the output](processing-messages.md#build-the-output-build). |
| Transform schema: `agent-docs/schemas/transform.schema.json` describes exactly the steps that run (`map`, `set`, `filter`, `destinationSet`, `build`); flow, destination, and response transforms are checked against it by the API and `config validate` | Implemented (wired) | `TestTransformSchema`, `TestValidateRefuses`, `TestSchemaPublishedAndMatchesTypes`. `destinationSet.include` is refused (D-20). |
| Delimited input: `inputFormat: delimited` (`delimited: {delimiter, header}`) gives transforms `{"rows": [...]}` (objects by column name, or lists) with RFC 4180 quoting | Implemented (wired) | `TestDelimitedInput`, `TestProcessDelimitedInput`, `TestDelimitedJSON`, `TestCheckInput`. One message per file; at most 100,000 rows; output is JSON. See [Transform delimited text](processing-messages.md#transform-delimited-text-csv). |
| XML input: `inputFormat: xml` gives transforms the document as JSON (`order.@id`, `order.patient.name.#text`, repeated names as lists, `#ns`); DTDs never processed and declared entities never expanded (predefined ones such as `&amp;` are decoded) | Implemented (wired) | `TestXMLInput`, `TestProcessXMLInput`, `TestXMLJSON`. Output is JSON unless a `build` step produces XML; order between different child names and mixed-content position not kept; at most 256 levels and 100,000 elements. See [Transform XML documents](processing-messages.md#transform-xml-documents). |
| HL7 v2 input: `inputFormat: hl7v2` gives transforms the message as JSON (`PID.5.1`, repetitions, every segment in order) | Implemented (wired) | `TestHL7Input`, `TestProcessHL7Input`, `TestHL7JSON`. Output is JSON unless a `build` step produces HL7 v2 again. See [Transform HL7 v2 messages](processing-messages.md#transform-hl7-v2-messages). |
| MLLP source: a flow accepts HL7 v2 over TCP (`source: {type: mllp, address}`) while it is started and answers each message with an HL7 ACK (AA stored, AR refused, AE not stored) | Implemented (wired) | `TestMLLPSource`, `TestMLLPHandler`, `TestMLLPServer`, `TestHL7ACKOptions`. No TLS or sender authentication yet; set `inputFormat: hl7v2` to transform the messages. See [Receive HL7 v2 over MLLP](processing-messages.md#receive-hl7-v2-over-mllp). |
| Other sources and destinations (database, SMTP, SOAP/REST web service, document, in-process inter-flow) | Library-only | Only file, http, and mllp sources and http, file, and mllp destinations are wired. |
| Per-flow and per-destination statistics (`GET /api/v1/flows/{id}/stats`) | Implemented (wired) | `TestStatsEventsTopology`. In memory; reset, dump, and time series are not available. |
| Event log with processing events (`GET /api/v1/events`) | Implemented (wired) | `TestStatsEventsTopology`. In memory, newest 10,000. Filters, get, count, max id, and export: see the Events row above. |
| Topology flow-node `activity` from real counters, zeros included | Implemented (wired) | `TestStatsEventsTopology`. Edge activity is not reported. |
| YAML DSL `map`, `set`, `filter` steps | Implemented (wired) | `TestPipelineEndToEnd`. `build` and `destinationSet` are not supported. |
| WASM executor (wazero), module registry | Library-only | Not used by the server. The executor has no WASI host. |
| Scheduler (durable jobs, leases, interval/cron) | Library-only | |
| Graceful shutdown bounded by `listen.shutdownTimeoutMs`; unfinished work resumes on the next start | Implemented (wired) | `TestGracefulShutdownRequeuesInFlightWork` |
| Delivery retries with persisted backoff, `dead-lettered` status, restart recovery | Implemented (wired) | `TestRetryRecoversQueuedMessage`, `TestQueuedWorkSurvivesRestart`, `TestDeadLetterAfterMaxAttempts`. Dead letters can be listed, requeued, and removed (next row). |
| Dead letters: list, show, requeue (one or all; delivered destinations not re-sent; attempt counts in the audit log and a `message.requeued` event), remove (`POST /api/v1/messages/{id}/requeue`, `POST /api/v1/messages/requeue`, `DELETE /api/v1/messages/{id}?status=dead-lettered`; CLI `deadletter list`, `show`, `requeue`, `remove`) | Implemented (wired) | `TestDeadLetterRequeue`. See [Dead-lettered messages](processing-messages.md#dead-lettered-messages). |
| Alerts and SMTP/webhook notifiers | Library-only | |
| In-server Git integration (repository, commit/log/diff/restore, push/pull, drift, plan/apply from a revision) and scheduled drift remediation | Enterprise (removed from the MVP, D-55) | Git is automation around the CLI and config files: keep `weavster.yaml` in your own repository and plan/apply it from CI (see [CI/CD](ci-cd.md)). `internal/gitstore` remains a library that the binary does not use. Flows in a config document (`flows.<id>`) use the same definition as the flow API and are validated against `flow.schema.json` (`TestConfigFlowIsAPIFlow`). |
| Legacy import | Unsupported | `internal/migrate` reads a made-up `<weavster-export>` XML schema, not any real legacy export format. |

## CLI

| Capability | Tier | Proof / notes |
|---|---|---|
| `weavster server [--config FILE] [address]` | Implemented (wired) | `TestSupportMatrixCLI/server-subcommand`, `TestServerConfigListen` |
| `weavster` with no subcommand | Implemented (wired) | `TestSupportMatrixCLI/no-subcommand`, `TestShell`. Opens the interactive remote shell (`weavster> ` prompt); `quit`, `exit`, or end of input ends it. Start the server with `weavster server`. |
| `weavster -h` | Implemented (wired) | `TestRunHelpAndVersion` |
| `weavster -v` | Implemented (wired) | `TestRunHelpAndVersion`, `TestShell/server_version`. Prints the server's version (and the client's). Needs credentials (`-u/-p` or `-c`); exits `2` if the server cannot be reached or refuses them. |
| `weavster -s script.txt` batch mode | Implemented (wired) | `TestSupportMatrixCLI/batch-*`, `TestFlowCLI`. Commands: `help`, `status`, `version`, `flow` (list, get, create, update, update-all, rename, enable, disable, remove, export, import, lifecycle actions, redeploy-all, start/stop-destination, connectors, ports, stats; flows by id or name), `deploy [timeout]`, `import "path" [force]`, `export <id or name or *> "path"`; double-quoted arguments, `user list/add/remove/changepw`, `quit`/`exit`. Exits `2` if any line fails (unknown command, usage error, unreadable file, unreachable server, or an error reply). See [Command-line client](cli.md). |
| `-a address` | Implemented (wired) | `TestSupportMatrixCLI/batch-*`, `TestShell`. Include the scheme, for example `http://127.0.0.1:8080` (default). |
| `-u`, `-p` | Implemented (wired) | `TestCLICredentials`, `TestShell`. Checked at startup; a failed login prints `Could not log in to server.` and continues. Sent as HTTP Basic credentials. |
| `-c connection-file` | Implemented (wired) | `TestShell`. YAML with `address`, `user`, `password`; `-a/-u/-p` override it; a missing or invalid file exits `2`. |
| `-d` | Implemented (wired) | `TestShell/debug_shows_causes`. Adds each underlying cause to error output. |
| Interactive shell | Implemented (wired) | `TestShell`. Same commands as batch mode. See [Command-line client](cli.md). |
| `weavster test [--filter NAME] [--format junit\|json] [--output DIR]` | Implemented (wired) | `TestRunTestCommand`. Runs four built-in codec round-trip fixtures (`identity/hl7`, `identity/json`, `identity/xml`, `identity/raw`). It does not discover your flows or fixtures. |

Example:

```bash
cat > smoke.txt <<'EOF'
status
flow list
EOF
weavster -a http://127.0.0.1:8080 -u admin -p 'PASSWORD' -s smoke.txt
```

Expected output: the `/api/v1/system` JSON document, followed by one line per flow: id, status, and name, separated by tabs. See [Command-line client](cli.md) for every command.

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

Codecs are library-only except HL7 v2, XML, and delimited text. The server reads HL7 v2
messages, XML documents, and delimited text for transforms ([`inputFormat`](processing-messages.md#transform-hl7-v2-messages)) and
acknowledges messages an [mllp source](processing-messages.md#receive-hl7-v2-over-mllp) receives;
transform output is always JSON. You can exercise the codecs with `weavster test`,
which covers HL7 v2, JSON, XML, and raw.

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

A failed destination is retried until `delivery.maxAttempts` (then `dead-lettered`), so wired
destinations are at-least-once: an attempt whose response was lost is sent again. The HTTP
destination sends the same `Idempotency-Key` on every attempt, so a receiver that honors it
sees each message once. Rows marked "library" describe adapters the server does not use yet. The
outbox library's `SemanticsForAdapter` labels non-TCP sinks "exactly-once", but that label is
not yet true.

| Adapter | Source | Sink | Guarantee | Sends idempotency key |
|---|---|---|---|---|
| File | wired | wired | at-least-once (a retry rewrites the same file name; a source file is read again if the server stops between storing the message and removing the file) | no |
| HTTP | library | wired | at-least-once; effectively once when the receiver honors `Idempotency-Key` | yes: `Idempotency-Key` header, the same for every attempt |
| TCP/MLLP | wired | wired | at-least-once: the source sends `AA` once the message is stored, and the destination repeats a delivery whose ACK was lost; either way the receiver may see a message twice | no (the protocol has no field for one) |
| Database | library | library | not wired | no |
| SMTP | — | library | not wired | no |
| Web service (SOAP/REST) | — | library | not wired | no |
| Document | — | library | not wired | no |
| In-process inter-flow | library | library | not wired | no |
| Broker, DICOM | Enterprise-deferred | Enterprise-deferred | — | — |
