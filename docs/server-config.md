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

A single-node server with a durable SQLite store and HTTPS:

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
  dialect: sqlite
  maxRetry: 3
  retryWaitMs: 1000
paths:
  dataDir: /var/lib/weavster     # SQLite file: /var/lib/weavster/weavster.db
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

## Keys

Keys you leave out keep their default.

### `listen`

| Key | Default | Description |
|---|---|---|
| `address` | `127.0.0.1:8080` | Cleartext HTTP `host:port`. The port is a number from 1 to 65535 or a service name such as `http`; `0` (a random port) is rejected. Set to `""` to serve HTTPS only. |
| `tlsAddress` | `""` (off) | HTTPS `host:port`, with the same port rules as `address`. Requires `tls.certFile` and `tls.keyFile`. |
| `requireMarkerHeader` | `true` | Require `X-Weavster-CSRF: 1` on every `/api/v1` request. Requests without it get `400`. |
| `shutdownTimeoutMs` | `10000` | On SIGINT/SIGTERM, how long to wait for in-flight requests and the retry worker before closing connections and exiting (1–600000). |

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
| `dialect` | `memory` | `memory`, `sqlite`, `postgres`, or `disabled`. |
| `dsn` | see below | Connection string. For `sqlite`, a file path. For `postgres`, a URL such as `postgres://user:pass@db:5432/weavster`. |
| `maxConnections` | `10` | Maximum open PostgreSQL connections. SQLite always uses one. |
| `maxRetry` | `3` | PostgreSQL only: extra connection attempts after the first failure. |
| `retryWaitMs` | `1000` | PostgreSQL only: wait between attempts, in milliseconds. |

- **`memory`**: messages, flow definitions, and users live in process memory and are lost on restart.
- **`sqlite`**: set `dsn` or `paths.dataDir`. `dsn` defaults to `<paths.dataDir>/weavster.db`.
  The parent directory is created (mode `0700`) if missing. Migrations run at startup. A SQLite
  failure is never retried.
- **`postgres`**: `dsn` is required. **PostgreSQL does not work yet:** the schema uses
  SQLite-only SQL, so startup fails after the retries.
- **`disabled`**: runs with no message store. `GET /api/v1/messages` returns `503 messages unavailable`.
  Flow definitions and users are kept in memory.

If every PostgreSQL attempt fails, the server exits `1`. SIGINT/SIGTERM during the retries
stops the server immediately with exit code `0`. When every attempt fails:

```text
Error: store: postgres: giving up after 4 attempts: ...
```

The store holds messages, flow definitions, and users. With `sqlite`, flows and users
(including password changes and lockouts) survive a restart.

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

### `stats`

| Key | Default | Description |
|---|---|---|
| `sampleIntervalMs` | `60000` | How often every flow's lifetime statistics are sampled for [statistics over time](processing-messages.md#statistics-over-time). |
| `retentionHours` | `24` | How long samples are kept. |

`sampleIntervalMs` must be 100–3,600,000 ms (one hour) and `retentionHours` 1–8760 (one year),
and together they may keep at most 100,000 samples per flow (for example, a 1-second interval
allows up to 27 hours). Samples are held in memory, at most 1,000,000 for all flows together; past
that the oldest are dropped.

```yaml
stats:
  sampleIntervalMs: 10000   # every 10 seconds
  retentionHours: 48
```

### `paths`

| Key | Default | Description |
|---|---|---|
| `dataDir` | `""` | Directory for the default SQLite file. Use an absolute path; a relative one resolves against the working directory, which is `/` under most service managers. |

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
