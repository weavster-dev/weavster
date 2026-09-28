# Production setup

What the server does by default to stay safe, and a configuration for running it in production.

## Secure defaults

Without any configuration the server:

- listens only on `127.0.0.1:8080`, so nothing outside the machine can reach it until you set
  `listen.address` or `listen.tlsAddress`;
- requires the `X-Weavster-CSRF` header on every API call (`listen.requireMarkerHeader: true`), so
  a web page cannot make a browser call it;
- has no default credentials: the first `admin` password comes from
  `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD` or `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE`, or is generated
  and printed once; a generated password must be changed at the first login, a password you
  provide is not forced to change (change it yourself, see the checklist and
  [Authentication](authentication.md));
- enforces a password policy (8 characters with upper case, lower case, and a digit) and locks an
  account for 5 minutes after 5 failed logins;
- accepts TLS 1.2 or later only, with modern cipher suites, when HTTPS is on;
- never follows HTTP redirects to a destination unless `maxRedirects` allows it, and never from
  `https` to `http`;
- verifies the certificate of every `https://` http destination and every mllp destination with
  `tls: true`, with no option to skip the check (database connections use TLS as their connection
  string's `sslmode` says: use `sslmode=verify-full`);
- reads secrets used by flows only from environment variables whose names start with
  `WEAVSTER_SOURCE_` or `WEAVSTER_DB_`, never from the flow definitions;
- refuses to run as root unless `WEAVSTER_ALLOW_ROOT=1` is set.

## A production configuration

HTTPS only with TLS 1.3, PostgreSQL over verified TLS, a stricter login policy, and more
retries. This is the file
[`docs/examples/production/weavster-server.yaml`](https://github.com/weavster-dev/weavster/blob/main/docs/examples/production/weavster-server.yaml)
from the repository, which a test starts the server from:

```yaml
# Production server configuration: HTTPS only, PostgreSQL, strict login policy.
# See docs/production.md. Secrets are not in this file: the store's password
# comes from ~/.pgpass of the server's account (a line for db.internal only),
# the first admin password from WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE, and
# flow connection strings from WEAVSTER_DB_* / WEAVSTER_SOURCE_* variables.
listen:
  address: ""                  # no cleartext listener
  tlsAddress: 0.0.0.0:8443
  requireMarkerHeader: true    # the X-Weavster-CSRF header (CSRF protection)
  shutdownTimeoutMs: 30000
tls:
  certFile: /etc/weavster/tls/server.crt
  keyFile: /etc/weavster/tls/server.key
  minVersion: "1.3"
store:
  dialect: postgres
  dsn: postgres://weavster@db.internal:5432/weavster?sslmode=verify-full
  maxConnections: 20
  maxRetry: 10
  retryWaitMs: 2000
paths:
  dataDir: /var/lib/weavster
auth:
  passwordPolicy:
    minLength: 14
    minUpper: 1
    minLower: 1
    minNumeric: 1
    minSpecial: 1
  lockout:
    retryLimit: 5
    lockoutPeriodSeconds: 900
delivery:
  maxAttempts: 10
  backoffBaseMs: 2000
  retryIntervalMs: 1000
processing:
  maxConcurrent: 64
  waitMs: 5000
flows:
  deployOnStartup: true
stats:
  sampleIntervalMs: 60000
  retentionHours: 168
```

Keep the secrets out of the file. The store's password goes in the server account's
`~/.pgpass`, on a line for the store's host only, so it is never sent to another database:

```text
db.internal:5432:weavster:weavster:the-store-password
```

Then start the server with the other secrets in its environment:

```bash
export WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/weavster-admin-password
export WEAVSTER_DB_WAREHOUSE="postgres://loader:$(cat /run/secrets/warehouse-password)@warehouse.internal/dw?sslmode=verify-full"
weavster server --config /etc/weavster/weavster-server.yaml
```

Do not set `PGPASSWORD` for the server: PostgreSQL clients use it for every connection string
without a password, so it would also be sent to the databases your flows read and write.

Clients then call the API over HTTPS with the marker header:

```bash
curl --cacert /etc/weavster/tls/ca.crt -u 'admin:…' -H 'X-Weavster-CSRF: 1' \
  https://weavster.internal:8443/api/v1/system
```

## Checklist

- Give the server a certificate from your CA, set `listen.address: ""`, and use
  `tls.minVersion: "1.3"` when every client supports it.
- Use PostgreSQL with `sslmode=verify-full`, for the store and for every `WEAVSTER_DB_…`
  connection string; keep the store's password in `~/.pgpass` (mode `0600`) of the server's
  account.
- Set `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE` for the first start, log in, change the password
  (a password you provide is not forced to change), remove the file, then create one account per
  person or system with only the permissions it needs.
- Run as an unprivileged account; keep `/etc/weavster` and `paths.dataDir` readable only by it.
- Give flows' http and mllp sources their own certificates (`certFile`, `keyFile`) and, for http
  sources, credentials (`username`, `passwordEnv`).
- Collect stderr: it holds the audit log and warnings such as a reached processing limit.
- Plan retention: messages are kept until you remove them (see [Capacity and limits](limits.md#retention)).
- Review the limits in [Capacity and limits](limits.md), especially `processing.maxConcurrent`,
  `delivery.maxAttempts`, and each destination's `timeoutMs`.
