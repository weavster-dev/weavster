# Server configuration

`weavster server` reads an optional YAML configuration file:

```bash
weavster server --config /etc/weavster/weavster.yaml
```

Without `--config`, the server uses the defaults below. A positional address overrides
`listen.address`, for example `weavster server --config weavster.yaml 127.0.0.1:9090`. Put
`--config` **before** the address. Anything after the address is rejected with
`Error: unexpected arguments`.

The file is checked strictly at startup. It must contain exactly one YAML document. An unknown
key, a value of the wrong type, an address that isn't `host:port`, or an invalid combination (for
example a password policy that forbids every character class) stops the server with exit code `1` and a message like:

```text
Error: config: /etc/weavster/weavster.yaml: yaml: unmarshal errors:
  line 3: field dialekt not found in type serverconfig.Store
```

## Example

A single-node server with a durable PostgreSQL store and HTTPS:

```yaml
listen:
  address: 127.0.0.1:8080        # cleartext listener; "" disables it
  tlsAddress: 0.0.0.0:8443       # HTTPS listener; "" disables it
  requireMarkerHeader: true
tls:
  certFile: /etc/weavster/tls/cert.pem
  keyFile: /etc/weavster/tls/key.pem
  minVersion: "1.2"
store:
  dialect: postgres
  dsn: postgres://weavster@db.internal:5432/weavster?sslmode=verify-full
  maxRetry: 3
  retryWaitMs: 1000
auth:
  passwordPolicy:
    minLength: 12
    minUpper: 1
    minLower: 1
    minNumeric: 1
    minSpecial: 1
  lockout:
    retryLimit: 5
    lockoutPeriodSeconds: 300
```

## Startup order

`weavster server` starts in this order:

1. Checks its arguments (a bad flag or an extra argument exits `2`) and refuses to run as root
   unless `WEAVSTER_ALLOW_ROOT=1` is set.
2. Reads and checks the configuration file (a problem exits `1`).
3. Opens the store and applies any pending schema migrations (see [Upgrades](#upgrades)).
   With PostgreSQL, a failed connection or migration is retried per `store.maxRetry` and
   `store.retryWaitMs`.
4. Loads users and creates the first `admin` account when there are none (see
   [Authentication](authentication.md)).
5. With `flows.deployOnStartup` (and a store), deploys every flow that is `enabled` and
   `undeployed` into its `initialState` (see [Flow lifecycle](flow-lifecycle.md#enabled-flows-start-automatically)).
   Flows keep the status they had otherwise.
6. Binds the API ports (`listen.address`, `listen.tlsAddress`). A port that is taken exits `1`
   here, before anything below starts, so no source has taken a file or a message.
7. Starts the background work, side by side: the recovery pass that resumes messages stored
   before the last stop (queued deliveries, and messages that were still being processed) for
   flows that are running, then retries every `delivery.retryIntervalMs`; and the running flows'
   sources. New messages can therefore be processed while older ones are still being recovered.
8. Answers API requests.

Messages of a flow that is not running are not recovered at start; they are finished once the
flow is started. The server keeps no jobs or leases of its own to reconcile: everything it must
resume is a stored message, which the recovery pass picks up.

## Upgrades

A new release upgrades the database schema when it starts (step 3 above), before it accepts any
traffic. The data is kept. Each schema change runs in its own transaction, so a failed upgrade
leaves the database at the last completed version, and the next start carries on from there.
Servers that start together on one database take turns: only the first applies the upgrade.
The database records which weavster release applied each schema version.

Some upgrades rebuild a table's indexes, and PostgreSQL locks the table while they run: other
servers using the same database wait for its messages until the upgrade ends (on a large store,
minutes). Upgrading to this release does this for the message tables on PostgreSQL (message ids
now compare byte by byte, the order searches use). Stop the other servers, or upgrade at a quiet
time.

A database that a **newer** release has already upgraded is refused, and nothing in it changes.
The server exits `1` at once (this is not retried) with:

```text
Error: store: state: the database schema is at version 10 (written by weavster 0.3.0), newer than this release supports (9): run weavster 0.3.0 or later, or restore a backup taken before the upgrade
```

So you can't roll back to an older release by just starting it again. Back up the database
before upgrading, and to go back, restore that backup and start the older release.

## Keys

Keys you leave out keep their default.

### `listen`

| Key | Default | Description |
|---|---|---|
| `address` | `127.0.0.1:8080` | Cleartext HTTP `host:port`. The port is a number from 1 to 65535 or a service name such as `http`; `0` (a random port) is rejected. Set to `""` to serve HTTPS only. |
| `tlsAddress` | `""` (off) | HTTPS `host:port`, with the same port rules as `address`. Requires `tls.certFile` and `tls.keyFile`. |
| `requireMarkerHeader` | `true` | Require `X-Weavster-CSRF: 1` on every `/api/v1` request. Requests without it get `400`. |
| `shutdownTimeoutMs` | `10000` | On SIGINT/SIGTERM, how long to wait for in-flight requests and the retry worker before closing connections and exiting (1–600000). |
| `contextPath` | `""` | Serve everything under this path, for example `/weavster` behind a proxy that routes by path: the API is then `/weavster/api/v1/…`, the OpenAPI document `/weavster/api/openapi.yaml`, and metrics `/weavster/metrics`. Other paths answer `404`. It starts with `/` and does not end with one. A flow's own HTTP source listens on its own address and is not affected. |

At least one of `address` and `tlsAddress` must be set.

### `tls`

| Key | Default | Description |
|---|---|---|
| `certFile` | `""` | PEM certificate (chain) for the HTTPS listener. |
| `keyFile` | `""` | PEM private key matching `certFile`. |
| `minVersion` | `"1.2"` | Lowest accepted TLS version: `"1.2"` or `"1.3"`. Quote the value so YAML reads it as a string. |

With TLS 1.2, only ECDHE AES-GCM cipher suites are offered. The certificate and key are loaded
before any listener starts. If they can't be read, the server exits `1` with `Error: tls: …`
and serves nothing.

### `store`

| Key | Default | Description |
|---|---|---|
| `dialect` | `memory` | `memory`, `postgres`, or `disabled`. |
| `dsn` | `""` | For `postgres`, a URL such as `postgres://weavster@db:5432/weavster?sslmode=verify-full`. |
| `dsnEnv` | `""` | For `postgres`, instead of `dsn`: the name of a [secret](#secrets) holding the URL, so its password stays out of this file. Letters, digits, and `_`. |
| `maxConnections` | `10` | Maximum open PostgreSQL connections. |
| `maxRetry` | `3` | PostgreSQL only: extra connection attempts after the first failure. |
| `retryWaitMs` | `1000` | PostgreSQL only: wait between attempts, in milliseconds. |

- **`memory`**: messages, flow definitions, and users live in process memory and are lost on restart.
- **`postgres`**: `dsn` or `dsnEnv` is required (not both), for example
  `postgres://weavster@db.internal:5432/weavster?sslmode=verify-full` (tested with PostgreSQL
  16). The server creates and migrates its tables on the first start; the account needs to create
  tables in the database (or in the schema named by `search_path` in the URL, for example
  `…&search_path=weavster`). Keep the password out of the file: put the whole URL in a secret
  and name it with `dsnEnv`, or put the password in `~/.pgpass` of the server's account, on a
  line for the database's host only. A missing secret counts as a failed connection attempt
  (`maxRetry`), and the server exits `1` naming it when the attempts run out.
- **`disabled`**: runs with no message store. `GET /api/v1/messages` returns `503 messages unavailable`.
  Flow definitions and users are kept in memory.
- **`sqlite`** is no longer a server store. The server exits `1` with
  `Error: config: store.dialect sqlite is no longer supported: use postgres for a durable store, or memory`.
  The `paths` section (`paths.dataDir` named the SQLite file's directory) was removed with it:
  delete it from the file, or the server exits `1` with `field paths not found`. See
  [Move from SQLite to PostgreSQL](#move-from-sqlite-to-postgresql).

If every PostgreSQL attempt fails, the server exits `1`. SIGINT/SIGTERM during the retries
stops the server immediately with exit code `0`. When every attempt fails:

```text
Error: store: postgres: giving up after 4 attempts: ...
```

The store holds messages, flow definitions, and users. With `postgres`, flows and
users (including password changes and lockouts) survive a restart.

#### Move from SQLite to PostgreSQL

To move a server from SQLite to PostgreSQL, save its setup **while the old release is still
running** (the new one does not start with `sqlite`):

1. Export the configuration with the config map:
   `GET /api/v1/config/export?includeConfigMap=true` (see [Export and import](config-transfer.md)).
2. Save every lookup group. `GET /api/v1/lookups` lists the groups with their number of
   entries, and `GET /api/v1/lookups/{group}?limit=10000` returns up to 10,000 entries of one as
   `{key: value}` (see [Dynamic lookups](lookups.md)). A group with 10,000 entries or more does
   not fit in one response: save it in parts with `prefix`, one part per first character of the
   keys (for example `prefix=A`, `prefix=B`, …), and split any part that returns exactly 10,000
   entries by a longer prefix (`prefix=AB`, …). Check that the saved entries of each group add up
   to its number of entries before you upgrade.
3. Upgrade, set `store.dialect: postgres` and `store.dsn`, remove `paths`, and start the server.
4. Import the configuration with `POST /api/v1/config/import?overwriteConfigMap=true`, and each
   group with `POST /api/v1/lookups/{group}/import`.

Messages and users are not moved: create the users again (see [Authentication](authentication.md)).

### `delivery`

| Key | Default | Description |
|---|---|---|
| `maxAttempts` | `5` | Attempts per destination, including the first, before a message is `dead-lettered`. |
| `backoffBaseMs` | `1000` | Delay before the first retry; it doubles for each further retry, up to one minute. |
| `retryIntervalMs` | `1000` | How often the server checks for retries that are due. |

`maxAttempts` must be 1–1000; the two intervals must be 1–3,600,000 ms (one hour). See [Processing messages](processing-messages.md#retries).

### `processing`

Bounds how many messages the server receives and processes at once, across the API and every
flow source, so a burst queues at the senders instead of piling up in the server.

| Key | Default | Description |
|---|---|---|
| `maxConcurrent` | `32` | Messages received and processed at the same time (1–10000). |
| `waitMs` | `5000` | How long a message that arrives while all are busy waits for its turn (0–60000; `0` refuses it at once). |

```yaml
processing: {maxConcurrent: 64, waitMs: 2000}
```

A message still waiting after `waitMs` is refused as busy, and the sender tries again:

- the API and http sources answer `503` with `Retry-After: 1` and
  `{"error":{"code":"SERVICE_UNAVAILABLE","message":"the server is busy: too many messages are being processed; retry shortly"}}`;
- an mllp source answers `AE` with `server busy`;
- a file or database source keeps the file or row and tries it again a moment later (this is
  throttling, not a failure: no `source.database.failed` event).

The server logs `processing limit reached: messages refused as busy` at most every 10 seconds
while it refuses messages, with how many it refused; a message waiting for a slot also stops
waiting when the server shuts down. A flow's own lock is taken first, so a flow being stopped or
changed holds up only its own messages, and a message for an unknown or stopped flow is answered
at once (`404`/`409`) without waiting.

A message a flow destination hands to another flow is processed in the sender's turn, so a chain
of flows never waits for itself. An http or mllp destination that sends to a source of the same
server needs a second turn while the sender holds its own: with every turn taken, such
deliveries are refused and retried. Use a `flow` destination to pass messages between flows of
one server. Retries of queued deliveries run one message at a time outside
this limit. Raise `maxConcurrent` when senders often see `503`/`AE` and the destinations can take
more parallel traffic; lower it to protect slow destinations or a small database.

### `flows`

| Key | Default | Description |
|---|---|---|
| `deployOnStartup` | `true` | At startup, deploy and start every flow that is `enabled` and `undeployed`. See [Flow lifecycle](flow-lifecycle.md#enabled-flows-start-automatically). |

### `prune`

Removes old messages from the store (see [Prune old messages](processing-messages.md#prune-old-messages)).
Off by default.

| Key | Default | Description |
|---|---|---|
| `maxAgeHours` | `0` (off) | Remove messages received more than this many hours ago (1–876000). |
| `maxMessages` | `0` (off) | Keep at most this many messages in the store; the oldest finished ones are removed first. Unfinished messages count toward the limit but are never removed. |
| `intervalMinutes` | `60` | How often a pass runs (1–10080, one week). The first pass runs one interval after the server starts. |
| `auditMaxAgeDays` | `0` (keep) | Remove stored [audit entries](audit-log.md#search-the-audit-log) older than this many days (1–36500). |
| `eventMaxAgeDays` | `0` (keep) | Remove stored [events](processing-messages.md#5-statistics-and-events) older than this many days (1–36500). |

Pruning needs a message store: with `store.dialect: disabled` a `prune` section stops the server at startup.

```yaml
prune: {maxAgeHours: 720, intervalMinutes: 60}   # keep 30 days
```

Only messages that are done are removed: `sent`, `filtered`, `errored`, and `dead-lettered`.
Messages that are `received`, `transformed`, or `queued` are never pruned.

### `stats`

| Key | Default | Description |
|---|---|---|
| `sampleIntervalMs` | `60000` | How often every flow's lifetime statistics are sampled for [statistics over time](processing-messages.md#statistics-over-time). |
| `retentionHours` | `24` | How long samples are kept. |

`sampleIntervalMs` must be 100–3,600,000 ms (one hour) and `retentionHours` 1–8760 (one year),
and together they may keep at most 100,000 samples per flow (for example, a 1-second interval
allows up to 27 hours). Samples are held in memory, at most 1,000,000 for all flows together; past
that the oldest are dropped. With `store.dialect: postgres` they are also stored, together with
each flow's current and lifetime statistics, and loaded again at start; stored samples older than
`retentionHours` are removed at every sample.

```yaml
stats:
  sampleIntervalMs: 10000   # every 10 seconds
  retentionHours: 48
```

### `secrets`

| Key | Default | Description |
|---|---|---|
| `dir` | `/run/secrets` | Where secret files are read from. An absolute path. |

A secret named `NAME` (a flow's `dsnEnv` or `passwordEnv`, or `store.dsnEnv`) is the server's
environment variable `NAME` or, when that is not set, the file `NAME` in `secrets.dir`. One
trailing newline in the file is ignored. `/run/secrets` is where Docker and Kubernetes mount
secrets, so a secret mounted there with the name a flow uses needs no other setup:

```yaml
store:
  dialect: postgres
  dsnEnv: WEAVSTER_STORE_DSN     # the file /etc/weavster/secrets/WEAVSTER_STORE_DSN
secrets:
  dir: /etc/weavster/secrets
```

```bash
install -m 0600 /dev/null /etc/weavster/secrets/WEAVSTER_STORE_DSN
echo 'postgres://weavster:the-password@db.internal:5432/weavster?sslmode=verify-full' > /etc/weavster/secrets/WEAVSTER_STORE_DSN
```

- An empty variable or file counts as not set. A missing secret is reported as
  `environment variable NAME is not set, and there is no file /etc/weavster/secrets/NAME`.
- Secrets are read when they are used (a database connection opens, an http source's port opens),
  so a changed file applies without a restart for database connections.
- Keep the directory readable by the server's account only (`chmod 0700`), and each file `0600`.

### `auth`

| Key | Default | Description |
|---|---|---|
| `passwordPolicy.minLength` | `8` | Minimum password length in characters (`0` = none). |
| `passwordPolicy.minUpper` / `minLower` / `minNumeric` / `minSpecial` | `1` / `1` / `1` / `0` | Required characters of each class. `-1` forbids the class. A special character is anything that is not a letter (of any script) or digit, spaces included. `GET /api/v1/system/password-requirements` shows the rules in words. |
| `lockout.retryLimit` | `5` | Failed logins before lockout (`0` = never lock). |
| `lockout.lockoutPeriodSeconds` | `300` | Lockout duration in seconds. |

These settings apply to the first-run `admin` password and to every login. See
[Authentication](authentication.md).

## Root accounts

The server refuses to run as `root`. Set the environment variable `WEAVSTER_ALLOW_ROOT=1` to
override this.
