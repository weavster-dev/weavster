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

### `git`

| Key | Default | Description |
|---|---|---|
| `path` | `""` | Directory of the server's [Git repository](git.md) of configuration. It is created, with an empty repository on branch `main`, when missing; an existing repository is used as it is. Empty turns the Git endpoints off (they answer `503`). |
| `remote.url` | `""` | The [remote repository](git.md#share-through-a-remote) to push to and pull from: an `https://` URL, a `file://` URL, or a local path. SSH (`ssh://`, `git@host:path`) and plain `http://` are refused at startup. Needs `path`. Must not contain a password. |
| `remote.username` | `""` | HTTPS user name. |
| `remote.passwordEnv` | `""` | Name of the environment variable holding the HTTPS password or access token. It is read at each push, pull, or status check, so a rotated token needs no restart. |

```yaml
git:
  path: /var/lib/weavster/config-repo
  remote:
    url: https://github.com/example/weavster-config.git
    username: x-access-token
    passwordEnv: WEAVSTER_GIT_TOKEN
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
