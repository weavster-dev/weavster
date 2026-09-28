# Production setup

What the server does by default to stay safe, and a configuration for running it in production.

## Secure defaults

Without any configuration the server:

- listens only on `127.0.0.1:8080`, so nothing outside the machine can reach it until you set
  `listen.address` or `listen.tlsAddress`;
- requires the `X-Weavster-CSRF` header on every API call (`listen.requireMarkerHeader: true`), so
  a web page cannot make a browser call it;
- has no default credentials: the first `admin` password comes from
  `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD`, `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE`, or is generated
  and printed once, and must be changed at the first login (see
  [Authentication](authentication.md));
- enforces a password policy (8 characters with upper case, lower case, and a digit) and locks an
  account for 5 minutes after 5 failed logins;
- accepts TLS 1.2 or later only, with modern cipher suites, when HTTPS is on;
- never follows HTTP redirects to a destination unless `maxRedirects` allows it, and never from
  `https` to `http`;
- verifies every certificate it connects to (TLS mllp and database destinations, `https://` http
  destinations), with no option to skip the check;
- reads secrets used by flows only from environment variables whose names start with
  `WEAVSTER_SOURCE_` or `WEAVSTER_DB_`, never from the flow definitions;
- refuses to run as root unless `WEAVSTER_ALLOW_ROOT=1` is set.

## A production configuration

HTTPS only with TLS 1.3, PostgreSQL over verified TLS, a stricter login policy, and more
retries. The file is also in the repository as
[`docs/examples/production/weavster-server.yaml`](https://github.com/weavster-dev/weavster/blob/main/docs/examples/production/weavster-server.yaml),
and a test starts the server from it:

```yaml
listen:
  address: ""                  # no cleartext listener
  tlsAddress: 0.0.0.0:8443
  requireMarkerHeader: true
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
  passwordPolicy: {minLength: 14, minUpper: 1, minLower: 1, minNumeric: 1, minSpecial: 1}
  lockout: {retryLimit: 5, lockoutPeriodSeconds: 900}
delivery: {maxAttempts: 10, backoffBaseMs: 2000, retryIntervalMs: 1000}
processing: {maxConcurrent: 64, waitMs: 5000}
flows: {deployOnStartup: true}
stats: {sampleIntervalMs: 60000, retentionHours: 168}
```

Start it with the secrets in the environment, not in the file:

```bash
export PGPASSWORD="$(cat /run/secrets/weavster-db-password)"
export WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/weavster-admin-password
export WEAVSTER_DB_WAREHOUSE='postgres://loader@warehouse.internal/dw?sslmode=verify-full'
weavster server --config /etc/weavster/weavster-server.yaml
```

Clients then call the API over HTTPS with the marker header:

```bash
curl --cacert /etc/weavster/tls/ca.crt -u 'admin:…' -H 'X-Weavster-CSRF: 1' \
  https://weavster.internal:8443/api/v1/system
```

## Checklist

- Give the server a certificate from your CA, set `listen.address: ""`, and use
  `tls.minVersion: "1.3"` when every client supports it.
- Use PostgreSQL with `sslmode=verify-full`; keep its password in `PGPASSWORD` or `~/.pgpass`
  readable only by the server's account.
- Set `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE` for the first start, log in, change the password,
  then create one account per person or system with only the permissions it needs.
- Run as an unprivileged account; keep `/etc/weavster` and `paths.dataDir` readable only by it.
- Give flows' http and mllp sources their own certificates (`certFile`, `keyFile`) and, for http
  sources, credentials (`username`, `passwordEnv`).
- Collect stderr: it holds the audit log and warnings such as a reached processing limit.
- Plan retention: messages are kept until you remove them (see [Capacity and limits](limits.md#retention)).
- Review the limits in [Capacity and limits](limits.md), especially `processing.maxConcurrent`,
  `delivery.maxAttempts`, and each destination's `timeoutMs`.
