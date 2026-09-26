# Server configuration

`weavster server` reads an optional YAML configuration file:

```bash
weavster server --config /etc/weavster/weavster.yaml
```

Without `--config`, the server uses the defaults below. A positional address overrides
`listen.address`, for example `weavster server --config weavster.yaml 127.0.0.1:9090`. Put
`--config` **before** the address: flags after the address are ignored.

The file is checked strictly at startup. An unknown key, a value of the wrong type, or an
invalid combination stops the server with exit code `1` and a message like:

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
| `address` | `127.0.0.1:8080` | Cleartext HTTP `host:port`. Set to `""` to serve HTTPS only. |
| `tlsAddress` | `""` (off) | HTTPS `host:port`. Requires `tls.certFile` and `tls.keyFile`. |
| `requireMarkerHeader` | `true` | Require `X-Weavster-CSRF: 1` on every `/api/v1` request. Requests without it get `400`. |

At least one of `address` and `tlsAddress` must be set.

### `tls`

| Key | Default | Description |
|---|---|---|
| `certFile` | `""` | PEM certificate (chain) for the HTTPS listener. |
| `keyFile` | `""` | PEM private key matching `certFile`. |
| `minVersion` | `"1.2"` | Lowest accepted TLS version: `"1.2"` or `"1.3"`. Quote the value so YAML reads it as a string. |

With TLS 1.2, only ECDHE AES-GCM cipher suites are offered. An unreadable certificate or key
stops the server with exit code `1`.

### `store`

| Key | Default | Description |
|---|---|---|
| `dialect` | `memory` | `memory`, `sqlite`, `postgres`, or `disabled`. |
| `dsn` | see below | Connection string. For `sqlite`, a file path. For `postgres`, a URL such as `postgres://user:pass@db:5432/weavster`. |
| `maxConnections` | `10` | Maximum open PostgreSQL connections. SQLite always uses one. |
| `maxRetry` | `3` | Extra connection attempts after the first failure. |
| `retryWaitMs` | `1000` | Wait between attempts, in milliseconds. |

- **`memory`**: messages live in process memory and are lost on restart.
- **`sqlite`**: `dsn` defaults to `<paths.dataDir>/weavster.db`. The directory is created
  (mode `0700`) if missing. Migrations run at startup.
- **`postgres`**: `dsn` is required. **PostgreSQL does not work yet:** the schema uses
  SQLite-only SQL, so startup fails after the retries.
- **`disabled`**: runs with no message store. `GET /api/v1/messages` returns `503 messages unavailable`.

If every attempt fails, the server exits `1`:

```text
Error: store: postgres: giving up after 4 attempts: ...
```

This setting stores **messages only**. Flow definitions are still kept in memory; see the
[support matrix](support-matrix.md).

### `paths`

| Key | Default | Description |
|---|---|---|
| `dataDir` | `data` (relative to the working directory) | Directory for the default SQLite file. |

### `auth`

| Key | Default | Description |
|---|---|---|
| `passwordPolicy.minLength` | `8` | Minimum password length (`0` = none). |
| `passwordPolicy.minUpper` / `minLower` / `minNumeric` / `minSpecial` | `1` / `1` / `1` / `0` | Required characters of each class. `-1` forbids the class. |
| `lockout.retryLimit` | `5` | Failed logins before lockout (`0` = never lock). |
| `lockout.lockoutPeriodSeconds` | `300` | Lockout duration in seconds. |

!!! note
    The server has no login endpoint yet, so these settings have no visible effect today. They
    apply to local users once authentication is wired.

## Root accounts

The server refuses to run as `root`. Set the environment variable `WEAVSTER_ALLOW_ROOT=1` to
override this.
