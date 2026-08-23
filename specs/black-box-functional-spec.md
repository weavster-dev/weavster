# Black-Box Functional Specification

**Status:** Draft for review
**Phase:** 2 (Greenfield re-architecture) — clean-room requirements
**Input:** Phase 1 source material + supporting deep-dives
**Method:** A subagent performed clean-room behavioral extraction of the Phase 1 source material; this document is the resulting pristine, implementation-agnostic requirements contract.

## Golden Rule

This document describes **only** user-visible behavior and system inputs/outputs. It contains **zero** references to any prior product/repo name, implementation language, build system, third-party library or framework, file path, class/module name, or version number. Every capability is expressed as a product-neutral requirement. Industry interchange standards (e.g., HL7 v2, X12, NCPDP, DICOM) are named only because they describe the *data* the system processes, not how it is built.

---

## 1. Product Purpose & Personas

### 1.1 Purpose

The system is a **message-oriented integration platform**. It receives messages from a wide variety of sources, applies filters and transformations, routes them to one or more destinations, and provides durable storage, search, export, scheduling, alerting, administration, and automation over those flows. It is operated through a command-line shell, a network management interface, and scriptable automation hooks, and is hardened for environments that handle sensitive or regulated data.

### 1.2 Personas

| Persona | Responsibilities |
|---|---|
| **Integration Engineer** | Authors and maintains message flows; configures sources/destinations; writes filter/transform logic; defines data-type handling; validates flows before deployment. |
| **Operations / Administrator** | Manages runtime lifecycle of flows (deploy/start/stop/pause); monitors statistics and logs; schedules/polls sources; manages retention and exports; performs config backup/restore. |
| **Compliance / Security Officer** | Administers users and roles; enforces password policy and lockout; audits access to protected data; reviews events/access logs; ensures TLS and transport hardening. |
| **Developer / Automator** | Drives flows and administration programmatically via CLI and API; maintains reusable code snippets and global scripts; integrates the platform into CI/CD pipelines and version-control. |

---

## 2. Use Cases

Behavior-only requirements. "The system MUST / SHALL …"

### 2.1 Authoring message flows
1. The system SHALL allow an authorized user to create a flow (pipeline) by specifying a source (listener/reader), an ordered set of zero or more destinations (writers/senders), and per-destination filters, transformers, and response handling.
2. The system MUST allow a flow to be defined in an undeployed (draft) state and SHALL persist that definition until explicitly removed.
3. The system SHALL allow naming, renaming, enabling, and disabling of flows, and SHALL preserve cross-flow dependencies when flows are imported/exported.

### 2.2 Transforming / filtering messages
4. The system MUST allow per-flow and per-destination filter and transform logic to be authored as script-based rules/steps and as declarative steps (field mapping, building/assembling output, XSLT-style conversion), executed in a defined order.
5. The system MUST support a "destination set" filter that excludes specific destinations from processing a given message.
6. The system MUST expose a set of reusable utility functions (message construction, response generation, serialization, date handling, hashing) available within flow logic.
7. The system SHALL support response-transformer and response-selector logic distinct from the initial filter/transform.

### 2.3 Routing between systems
8. The system MUST route each accepted message to every enabled destination that is not excluded by the destination-set filter.
9. The system SHALL support routing a message into another flow in-process (inter-flow routing) by name or identifier, including from within user logic.
10. The system SHALL allow per-destination transmission modes and protocol-specific options (e.g., length-framed transmission).

### 2.4 Scheduling / polling sources
11. The system MUST allow a polling/scheduled source to specify an interval and SHALL trigger acquisition at that interval.
12. The system SHALL support scheduled/recurring acquisition in interval or cron-like form.

### 2.5 Retry / queuing
13. The system MUST support per-destination queuing of messages that fail to send, with configurable retry behavior.
14. The system SHALL track, for each queued message and destination, the number of send attempts and the last-failure error code.
15. The system MUST support returning a failed message to a queued state and SHALL allow the queue to be processed later.
16. The system SHALL recover/process messages that remain queued after a restart or outage.

### 2.6 Message persistence, search & export
17. The system MUST store received, intermediate, and sent message content per a configurable storage policy and per-content-type options.
18. The system SHALL allow searching stored messages by identifier ranges, date range, status, send-attempt count, content subtypes, metadata values, and custom metadata columns, with pagination and sorting.
19. The system MUST support exporting messages in multiple content forms (raw, processed raw, transformed, encoded, response, original) with optional archive, compression, and encryption.
20. The system MUST support importing messages back into the store (from a path or archive) for reprocessing.
21. The system MUST support reprocessing stored messages (individually, by filter, or by reference to a prior flow) and SHALL record results as new message content.
22. The system MUST allow removal of messages (individually, by search results, or all for a flow) and SHALL restart running flows when clearing all messages if required.
23. The system SHALL provide data pruning by age and/or size, with start/stop/status control.

### 2.7 Alerting
24. The system MUST allow alert definitions triggered by message-processing errors (and related conditions), with configurable triggers, recipients, and flow/source scope.
25. The system SHALL allow alerts to be enabled/disabled, imported/exported, and tested, and SHALL notify configured recipients when a trigger fires.

### 2.8 User & role administration
26. The system MUST support creating, listing, updating, and removing user accounts (name, organization, email, credential) and password changes.
27. The system MUST enforce per-user permissions scoped by resource category (alerts, flows, messages, events, code snippets, global scripts, config map, settings, extensions, resources, etc.).
28. The system MUST support an external authorization hook that can override built-in credential validation, and a pluggable multi-factor hook invoked after successful primary authentication.

### 2.9 Configuration import/export
29. The system MUST support exporting/importing flows (single or bulk), full system configuration, alerts, global scripts, code snippets/libraries, and the config map, to/from files.
30. The system SHALL support importing full configuration with options to suppress deployment and to overwrite the existing config map.
31. The system SHALL detect and surface file-not-found and import-overwrite conflicts (with a force mode).

### 2.10 Scriptable automation / CI
32. The system MUST provide a scriptable command shell (interactive and batch-from-script-file).
33. The system SHALL support programmatic client automation over the network interface.
34. The system SHALL support version-controlled (git-style) management of flows, code snippets/libraries, and global scripts — commit, push, pull, history, working-tree diff, restore.

### 2.11 Observability (events / statistics / logs)
35. The system MUST record an event log of administrative and operational actions, with search, count, and export.
36. The system MUST collect per-flow statistics (received, filtered, transformed, sent, errored, queued, connector-level counters) with reset (current and/or lifetime) and dump-to-file.
37. The system SHALL provide time-series statistics for trending and a server log viewer.
38. The system MUST expose system status (identifier, version, build date, timezone, time, runtime info, charsets, protocols/cipher suites, license info).

### 2.12 Versioning of configuration
39. The system MUST keep historical revisions of versioned configuration artifacts and SHALL allow viewing file history, content-at-revision, and repository log.
40. The system MUST support committing/pushing selected artifacts, pulling remote changes (remote-wins conflicts), and restoring from backup.

### 2.13 Security hardening
41. The system MUST enforce a configurable password policy (min length, character-class requirements, expiration, grace period, reuse constraints) on creation/change.
42. The system MUST enforce account lockout after a configurable number of failed attempts for a configurable period.
43. The system MUST be able to return a generic login-failure message that does not reveal username existence or lockout state (anti-enumeration).
44. The system MUST support HTTPS with configurable TLS protocols/ciphers and a managed credential store.
45. The system MUST emit transport-hardening headers (clickjacking, CSP, HSTS, content-type sniffing) and reject cross-site requests lacking the required marker header.

---

## 3. Generic CLI Surface

The CLI is a **remote shell** operating over the platform's network API (not a standalone runtime). Renamings applied: "channel" → **flow**, "code template" → **code snippet**, "configuration map" → **config map**.

### 3.1 Startup flags

| Flag | Argument | Purpose |
|---|---|---|
| `-a` | address | Server address to connect to |
| `-u` | user | Login username |
| `-p` | password | Login password |
| `-s` | script | Script file (batch mode) |
| `-v` | version | Server version |
| `-c` | config | Path to default connection/config file |
| `-h` | — | Print usage/help and exit |
| `-d` | — | Debug mode (print stack traces on error) |

### 3.2 Commands

| Command | Args | Purpose |
|---|---|---|
| `help` | — | List commands |
| `status` | — | Show deployed-flow status |
| `deploy` | `[timeout]` | Deploy all flows |
| `flow` | `list` | List flows |
| `flow` | `deploy\|undeploy` `id\|name` | Deploy/undeploy a flow |
| `flow` | `start\|stop\|halt\|pause\|resume` `id\|name` | Change runtime state |
| `flow` | `enable\|disable` `id\|name` | Enable/disable |
| `flow` | `rename` `id\|name` `newname` | Rename |
| `flow` | `remove` `id\|name` | Remove |
| `flow` | `stats` `[id\|name]` | Statistics |
| `import` | `"path" [force]` | Import flows |
| `export` | `id\|"name"\|* "path"` | Export flows |
| `importcfg` | `"path" [nodeploy] [overwriteconfigmap]` | Import full config |
| `exportcfg` | `"path" [overwriteconfigmap]` | Export full config |
| `importalert` | `"path" [force]` | Import alerts |
| `exportalert` | `id\|"name"\|* "path"` | Export alerts |
| `importscripts` | `"path"` | Import global scripts |
| `exportscripts` | `"path"` | Export global scripts |
| `snippet` | `list\|import\|export\|remove` | Manage code snippets |
| `snippet library` | `list\|import\|export\|remove` | Manage snippet libraries |
| `importmessages` | `"path" id` | Import messages into a flow |
| `exportmessages` | `"pattern" id [format] [pageSize]` | Export messages (format: raw/processedraw/transformed/encoded/response/…) |
| `importmap` | `"path"` | Import config map |
| `exportmap` | `"path"` | Export config map |
| `clearallmessages` | — | Remove all messages (restarts running flows) |
| `resetstats` | `[lifetime]` | Reset flow statistics |
| `dump` | `stats\|events "path"` | Dump statistics/events to file |
| `user` | `list\|add\|remove\|changepw` | User administration |
| `quit` | — | Exit shell |

### 3.3 Exit-code semantics

- `0` — success, or help requested/displayed.
- `2` — usage or configuration error (parse error, missing required args, or missing config/connection file).
- Login failure prints "Could not log in to server." and returns to the prompt **without** a non-zero exit.

---

## 4. Generic Configuration Schema

Grouped keys with generic names, purpose, and type. No specific database product or library is named.

### 4.1 Networking / HTTP(S) / TLS

| Key | Purpose | Type |
|---|---|---|
| listen host / port / tls-port / context-path | Bind addresses, cleartext + TLS listener ports, URL context path | string / int / int / path |
| TLS client protocols, TLS server protocols, cipher suites | Negotiable protocol and cipher selection | enum-list |
| ephemeral-DH-key-size | Ephemeral DH key size | int |
| credential-store path / passphrase / type | Server credential store location and type | path / string / enum |
| require-request-marker-header | Require cross-site-request marker on API calls | bool |
| CORS origin/methods/headers/credentials/expose-headers/max-age | Cross-origin policy | string / string / string / bool / string / int |
| strict-transport-security | Emit HSTS header | bool |
| frame-options / content-security-policy | Clickjacking/embedding headers | enum / string |

### 4.2 Database

| Key | Purpose | Type |
|---|---|---|
| database dialect | Primary database dialect selector | enum |
| embedded-mode | Use embedded file-based store (create on demand; optionally disable auto format upgrades) | bool |
| connection-url / username / password / driver / pool | Connection endpoint and pool selection | string / string / string / enum / enum |
| max-connections | Write-path max concurrent connections | int |
| read-only split (url/user/pass/max-conns) | Independent read-only pool for search/export/reads | string / int |
| max-retry / retry-wait-ms | Connection retry count and delay | int / int |
| enable read-write split | Route reads to the read-only pool | bool |

### 4.3 Paths / directories

| Key | Purpose | Type |
|---|---|---|
| application-data directory | Root dir for persistent app data | path |
| temporary-data directory | Scratch dir (defaults under app-data) | path |
| config-map file path | Location of config-map file | path |
| credential-store file path | Location of credential store | path |
| management-console heap size | Memory limit for the management console | size |

### 4.4 Security / password policy

| Key | Purpose | Type |
|---|---|---|
| password min-length | Minimum password length (0 = none) | int |
| password min-upper/lower/numeric/special | Required character-class counts (−1 forbids the class) | int |
| password expiration / grace-period | Days to expiry and grace window | int |
| password reuse-period / reuse-limit | Reuse constraints against prior credentials | int |
| password retry-limit / lockout-period | Failed-attempt threshold and lockout duration (0 = disabled) | int |
| allow-username-enumeration | Whether failures reveal account-specific messages (default off) | bool |
| allow-privileged-run | Whether to refuse running under a privileged OS account (default refuse) | bool |

### 4.5 Feature toggles

| Key | Purpose | Type |
|---|---|---|
| deploy-flows-on-startup | Auto-deploy flows at startup | bool |
| include-external-libraries-on-deploy | Load bundled/external runtime libraries at deploy | bool |
| script-language-version | Language feature level for user logic | enum |
| log-flow-context | Include flow context in log lines | bool |
| log-error-events | Emit event-log entries for processing errors | bool |

---

## 5. Generic API Resource Contracts

Exposed over an HTTP API (XML + JSON), versioned and unversioned (unversioned resolves to latest). Operations are synchronous by default; some long-running operations may be asynchronous. Sensitive parameters can be excluded from audit capture.

| Resource | Verbs | Key operations |
|---|---|---|
| **Flows (pipelines)** | GET/POST/PUT/DELETE | Create/list/update/remove; connector names; metadata columns; ids-and-names; set enabled/initial-state; ports-in-use; summary; bulk update; deploy/undeploy/redeploy-all; start/stop/halt/pause/resume (all, per-flow, per-connector) |
| **Flow statistics** | GET/POST | Get (all/per-flow); clear (per-flow, all, lifetime); reset |
| **Messages** | GET/POST/DELETE | Search (rich filters); get message/attachments/content; reprocess (by id/filter/bulk); import/export (archive/compress/encrypt); remove; audit access to protected content |
| **Alerts** | GET/POST/PUT/DELETE | List/create/update/remove; info/statuses; enable/disable; options |
| **Events** | GET/POST | List/get; search/count; max-event-id; export |
| **Users** | GET/POST/PUT/DELETE | Login/logout; list/get/current; create/update/remove; check/set password; preferences; logged-in status; acknowledge notification |
| **Code snippets (+ libraries)** | GET/POST/PUT/DELETE | Snippet and library CRUD; bulk update; summary |
| **Global scripts** | GET/PUT | Read/update the global script set |
| **Config map** | GET/PUT | Read/update the key/value config map |
| **System info** | GET | id, version, build date, status, timezone, time, runtime info, about, charsets, public settings, password requirements, protocols/ciphers, script-language version, resources, license info, generate GUID |
| **Settings** | GET/PUT | Server settings, database drivers, flow dependencies/metadata/tags; encryption ops; test email |
| **Plugins / extensions** | GET/POST/PUT | Upload/install/uninstall/list; enable/disable; per-extension properties; connector/plugin inventory |
| **Statistics** | GET | Time-series statistics |
| **Config backup/restore** | via export/import | Full config export; import with nodeploy + overwrite-config-map |
| **Per-connector tests** | POST | File read/write test; DB list-tables; TCP/HTTP test connection; SMTP test email; document write test; queue templates; web-service WSDL/definition/envelope/SOAP-action/test |
| **Git-style version control** | GET/POST | File history; content-at-revision; load artifacts; commit+push flows/snippets/scripts; write to working tree; save libraries; repo info/changes/log; restore; remote status; pull/push; validate settings |
| **Dynamic lookups** | GET/POST | Managed key/value lookup gateway (get/matching/batch/exists/set/delete/import) |
| **Message trends** | GET | Aggregated message-trend analytics |

---

## 6. State Transitions

### 6.1 Flow lifecycle

```
undeployed (draft) → deployed → started → (paused | halted) → stopped → removed
```

- A flow is authored and persisted in the **undeployed** state; it may be enabled/disabled while undeployed.
- **Deploy** transitions undeployed → deployed and re-deploys its dependencies.
- **Start** transitions deployed → started and begins processing.
- A started flow may be **paused** (suspended, resumable) or **halted** (force-stopped); each recovers to started via resume.
- **Stopped** is the terminal running-state; a flow can be started from stopped.
- **Removed** deletes the definition; running flows are undeployed first.
- **Undeploy** returns a started/stopped flow to undeployed; **redeploy-all** undeploys then re-deploys all.
- Enabled/disabled is an orthogonal flag for automatic deployment eligibility.
- Per-destination (connector) start/stop is supported independently.

### 6.2 Message processing

```
received → filtered → transformed → routed → delivered / sent / queued / errored
```

- A message is received; its filter is evaluated. Rejected messages are marked **filtered** and not routed.
- Accepted messages are transformed, then routed to each enabled, non-excluded destination.
- Per destination, the outcome is **sent**, **queued** (retained for retry), or **errored** (failure with accumulated error).
- A message yields a per-destination status plus an aggregate status.

### 6.3 Per-destination send-attempts

Each (message × destination) pair carries a **send-attempt count** and a **last error code**. Attempts increment on each delivery try; queries filter by min/max attempt count. Failure retains the message in the queue for retry/recovery; eventual success marks it sent; reprocessing generates new content recording the result.

---

## 7. Data Types

| Type | Description |
|---|---|
| Delimited text | Tab-/pipe-/delimited records parsed into rows/columns with configurable delimiters. |
| Structured health record (HL7 v2) | Segment-based, length-framed health text with acknowledgment generation and per-version models. |
| XML | Element-based parse/serialize. |
| JSON | Structured parse/serialize. |
| X12 EDI | ANSI X12 transactions (segments, loops, enveloping). |
| NCPDP pharmacy billing | Pharmacy-claims records with specialized number formatting. |
| Medical imaging (DICOM) | Binary imaging datasets with element tags and study/series/instance metadata. |
| Binary / raw | Untyped bytes passed through. |
| Clinical document (HL7 v3 / XML) | Hierarchical XML-based clinical documents. |

Each type supports an associated serializer (parse/format) and, where applicable, acknowledgment generation.

---

## 8. Source / Sink Adapters

| Adapter | Description |
|---|---|
| File | Read/write messages as files with patterns/filters and recursive traversal. |
| HTTP | Receive requests (listener) or send requests with configurable method + auth. |
| TCP (length-framed) | Accept/connect raw sockets with length-framed transmission modes. |
| In-memory inter-flow | Route messages between flows in-process without network I/O. |
| Database | Query a table/view (source) or write (sink) with scheduling and parameterized queries. |
| Message queue | Publish/consume from a broker-backed queue/topic. |
| Script | Execute custom user logic as a source generator or sink handler. |
| Email (SMTP) | Send mail via SMTP with optional auth and TLS. |
| Web service (SOAP/REST) | Consume/expose services via WSDL-derived or generic HTTP operations. |
| Medical imaging | Exchange DICOM datasets over the network using application-entity titles. |
| Document writer | Render messages into documents via a template. |

---

## 9. Runtime Behavior

- **Pipeline.** Each flow executes: ingest → filter → transform → route → deliver (or queue/error); response processing and selection follow delivery.
- **Scheduling.** Polling sources fire on interval/cron; scheduled jobs are recovered after restart.
- **Retry/queuing.** Destinations enqueue failed sends and retry; per-destination attempt/error counters are maintained.
- **Persistence backends** (configurable per flow/connector): **durable** (database-backed), **buffered** (in-memory buffering in front of the store), **timed** (scheduled operations for polling sources), **passthrough** (no persistence).
- **Purging.** A pruner removes stored messages/events by age/size; controllable start/stop/status.
- **Export/archive.** Messages/events export to archives with archive format, compression, and optional encryption; import restores them.
- **Reprocessing.** Stored messages re-run through a flow (individually/by filter/bulk); results stored as new content; protected-content access is audited.
- **Metadata.** Per-connector custom metadata is stored and searchable; content search scoped by subtype, metadata column, and map-based fields.

---

## 10. Security & Observability Behaviors

- **Authentication.** Built-in credential validation or delegation to a pluggable external authorization provider; a pluggable multi-factor hook runs after primary authentication.
- **Password policy.** Min length, character-class minimums (−1 forbids), expiration/grace, reuse constraints; enforced on create/change; credential history retained per policy.
- **Account lockout.** Lock after N failures for a period; strikes decay after the window; remaining attempts/time surfaced (subject to anti-enumeration).
- **Anti-enumeration.** When enabled (default), all failures return an identical generic message.
- **TLS.** HTTPS with configurable client/server protocols/ciphers, ephemeral DH sizing, managed credential store; storage-layer retry with backoff.
- **CSRF.** API requests must carry the cross-site-request marker header, else HTTP 400.
- **CORS.** Configurable origin/methods/headers/credentials/expose-headers/max-age; omitted when unset.
- **Security headers.** HSTS, X-Frame-Options (DENY), CSP (frame-ancestors 'none'), X-Content-Type-Options (nosniff); TRACE/TRACK blocked (405).
- **XXE-safe parsing.** Disables external entity resolution, DTD expansion, and external DTD loading.
- **PHI / audit access logging.** Access to and queries of protected message content are written to the audit/event log; sensitive parameters excluded.
- **Events.** Event log with search/count/export; optional flow-context and error-event entries.
- **Statistics.** Per-flow received/filtered/transformed/sent/queued/errored counters; resettable; dumpable.
- **Time-series statistics.** Historical trend data.
- **Server logs.** Log viewer; logging level/config externally selectable.

---

## 11. Edge Cases & Error Behaviors

- **Exit codes.** Shell: `0` success/help; `2` usage/missing-arg/missing-config. Clean server shutdown exits `0`. Some bundled utilities use `0/1/2`.
- **Error semantics.** Errors prefix with "Error:"; stack traces only in debug mode; unknown command/flow-subcommand produce explicit messages; login failure returns without non-zero exit.
- **Deprecated commands.** Print a deprecation warning directing to the replacement, then still execute (redirected).
- **Error aggregation.** Downstream destination errors accumulate (separator-joined) into the processing error, preserving the full trail.
- **Fallbacks.** Configurable lower-layer charset fallback; embedded HTTP request/response timeouts default if unset; ephemeral DH key size hardened if unset; bounded outbound redirect count.
- **Privileged-run guard.** Refuses to run under a privileged OS account unless allowed; terminates non-zero with a "use a dedicated service account" message; one platform's privilege probe fails open on timeout.
- **Storage connection.** Retries with configurable count/delay; a disabled/no-connection mode supports running without the primary store.
- **Import conflicts.** Force flag for overwriting; missing files reported explicitly.

---

## 12. Non-Functional Requirements (behavioral floor)

- **Correctness.** Message content, status, and side effects must be traceable and reproducible for audit.
- **Durability.** Durable-backend messages must survive process restart; queued messages must be recoverable.
- **Security.** Regulated-data handling requires TLS in transit, encrypted-at-rest credentials, and auditability of protected-content access.
- **Operability.** All administrative actions available via CLI and API; statistics/logs/events sufficient to operate without the source code.
- **Portability.** The system must run on a single host and be installable/updatable without requiring a specific developer toolchain on the target host.

---

## 13. Open Questions (to resolve in Phase 2 architecture)

1. Whether the management console is a graphical application, a web UI, or CLI-only (behavioral parity is assumed; form factor is an architecture decision).
2. The exact per-message persistence granularity (full content vs metadata-only vs per-content-type) and its default.
3. The default retention window and pruning defaults for stored messages/events.
4. Whether "scripted user logic" (use case 4/6) is subject to a single language or multiple languages — this is a technical constraint to be imposed in the architecture, not a behavioral requirement here.

---

*This document is intentionally implementation-free. The subsequent target-architecture document imposes the non-negotiable stack constraints and maps each behavior above to a concrete new implementation.*
