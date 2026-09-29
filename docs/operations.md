# Operations

How to back up and restore a server, watch it, find out why it fails, and recover from an
incident. Upgrading and rolling back are in [Install, upgrade, and roll back](install.md).

## What to back up

| What | Where | How |
|---|---|---|
| Messages, flows, users, alerts, snippets, config map, scripts, settings, lookups, the audit log, events, statistics | The store's PostgreSQL database (`store.dialect: postgres`) | `pg_dump` (below) |
| The server configuration | The file you pass to `--config` | Copy the file |
| TLS certificates and keys | `tls.certFile`, `tls.keyFile`, and each source's `certFile`/`keyFile` | Copy the files (keep them private) |
| Secrets | `secrets.dir` (default `/run/secrets`), `~/.pgpass` of the server's account, environment variables | However you manage secrets |
| Files a flow reads or writes | A file source's `dir` and `moveTo`, a file destination's `dir` | Copy the directories |

`ops.yaml` in the examples is a [connection file](cli.md) with the address and an operator's
credentials.

With `store.dialect: memory` (the default) or `disabled`, nothing survives a restart: there is
nothing to back up but the files. Use `postgres` for any server whose data you need to keep.

Weavster has no backup command of its own: back up the database with PostgreSQL's tools.

### Back up the database

```bash
pg_dump -Fc -f weavster-2026-09-29.dump 'postgres://weavster@db.internal:5432/weavster?sslmode=verify-full'
```

`pg_dump` takes a consistent snapshot while the server runs; you do not need to stop it. Take one
before every upgrade (it is what makes a [rollback](install.md#roll-back) possible) and on a
schedule that matches how many messages you can afford to lose.

### Restore the database

1. Stop the server.
2. Restore the dump into the store's database:

   ```bash
   pg_restore --clean --if-exists -d 'postgres://weavster@db.internal:5432/weavster?sslmode=verify-full' weavster-2026-09-29.dump
   ```

3. Start the same release that took the backup, or a newer one (it upgrades the schema at start).

Messages received after the backup are not in it. Export them first if you can still reach the
server (the client's `exportmessages "path" *` writes the newest 10,000; see
[Export and import messages](processing-messages.md#export-and-import-messages) for more), and
import them after the restore.

Sign-in tokens are not kept across restarts: users log in again after a restore.

### Keep a copy of the configuration too

A configuration export is a portable file of flows, alerts, snippets, scripts, and settings that
you can import into another server or keep in version control. It does not hold users,
passwords, lookups, or messages, so it does not replace the database backup.

```text
$ weavster -c ops.yaml
weavster> exportcfg "weavster-config.json"
```

See [Configuration export and import](config-transfer.md), or manage the configuration from files
with [Config-as-code documents](config-as-code.md).

## Monitor

### Is it up?

Weavster has no separate health endpoint. For a liveness probe, request the API description; it
needs no login:

```bash
curl -fsS -o /dev/null http://127.0.0.1:8080/api/openapi.yaml && echo up
```

With `listen.contextPath: /weavster`, the path is `/weavster/api/openapi.yaml`. The server opens
its ports only after it has connected to the store and upgraded the schema, so once it answers,
the API is ready.

For more detail, `GET /api/v1/system` (any signed-in user) returns the version and uptime:

```bash
curl -s -u 'ops:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/system
```

See [System information](system-info.md).

### Metrics

Scrape `GET /metrics` with an account that has `flows:view`; see [Metrics](metrics.md) for the
Prometheus configuration and every metric. Alert on:

| Signal | Metric | Meaning |
|---|---|---|
| Deliveries failing | `weavster_connector_messages_total{outcome="errored"}` rising | A destination rejects messages or cannot be reached; they are retried, then dead-lettered |
| Server at capacity | `weavster_processing_refused_total` rising, or `weavster_processing_in_flight` equal to `weavster_processing_slots` | Messages are refused as busy; raise `processing.maxConcurrent` or add capacity |
| Flows not running | `weavster_flows{status="started"}` lower than expected | A flow was stopped, or failed to deploy at start |

Alerts you define in Weavster are stored but do not fire in this edition; alert from your
monitoring system instead.

### Logs

The server writes its log to stderr as text lines (`time=… level=INFO msg=…`). The log level and
format cannot be changed. Collect stderr: it holds the audit log (`msg=audit`), the first-run
admin password, and warnings such as:

| Log line | Meaning |
|---|---|
| `store connection failed` | At start, the store cannot be reached; the server tries again, up to `store.maxRetry` times |
| `processing limit reached: messages refused as busy` | Every processing slot is in use |
| `delivery retry pass failed` | A pass over queued deliveries failed (often because the store cannot be reached); it runs again on the next interval |
| `auto-deploy failed` | A flow in the store could not be deployed at start |
| `audit entry not stored` | An audit entry was logged to stderr but not stored, so `GET /api/v1/audit` lacks it |
| `shutdown deadline reached; unfinished messages are stored and resume on the next start` | The server stopped before every message was done |

API errors carry no request ID; match an error to the log by time and by the user and resource
in the audit line.

## Troubleshoot

The server prints why it failed to start on stderr, as one `Error:` line, and exits `1` (`2` for
a usage error).

| Error | Fix |
|---|---|
| `Error: listen tcp 127.0.0.1:8080: bind: address already in use` | Another process (often another Weavster) has the port: stop it, or change `listen.address` |
| `Error: config: …: yaml: unmarshal errors:` | The next line names a misspelled or unknown key in the configuration file, for example `line 2: field dialekt not found in type serverconfig.Store`; see [Server configuration](server-config.md) |
| `Error: config: the postgres dialect needs store.dsn or store.dsnEnv (one of them)` | Set exactly one of them |
| `Error: store: postgres: giving up after N attempts: store.dsnEnv: environment variable NAME is not set, and there is no file /run/secrets/NAME` | Provide the secret as a variable or a file in `secrets.dir` (default `/run/secrets`) |
| `Error: store: postgres: giving up after N attempts: …` | The database cannot be reached: check the host, port, credentials, and `sslmode` in the connection string |
| `Error: store: state: the database schema is at version N (written by weavster X), newer than this release supports (M): …` | A newer release upgraded the database: run that release, or [restore](#restore-the-database) a backup taken before the upgrade |
| `Error: tls: open …: no such file or directory` | A path in `tls.certFile` or `tls.keyFile` is wrong (`permission denied` instead of `no such file or directory`: the server's account cannot read it) |
| `Error: refusing to run under a privileged OS account; use a dedicated service account or set WEAVSTER_ALLOW_ROOT=1` | Run the server as an unprivileged account |

When the server runs but messages do not arrive:

1. The client's `status` command (or `GET /api/v1/flows`) shows whether the flow is started. A flow must be
   deployed and started to process messages; see [Flow lifecycle](flow-lifecycle.md).
2. `GET /api/v1/messages?flowId=FLOW&status=errored` lists messages that failed, with the error of
   each; `deadletter list FLOW` lists those that ran out of delivery attempts.
3. `GET /api/v1/events` shows what happened, such as `message.errored` for a failed transform or
   `source.http.failed` for a source that could not listen; see
   [Statistics and events](processing-messages.md#5-statistics-and-events).

## Recover from an incident

### A destination was down

Deliveries to an unreachable destination are retried with backoff (`delivery.backoffBaseMs`,
doubling up to one minute) and, after `delivery.maxAttempts` attempts, the message is
dead-lettered. Once the destination is back, send the dead letters again:

```text
$ weavster -c ops.yaml
weavster> deadletter requeue all adt
```

A requeued message goes only to the destinations that did not receive it. See
[Dead-lettered messages](processing-messages.md#dead-lettered-messages).

To hold messages while you fix a destination, stop that destination (`POST
/api/v1/flows/{id}/destinations/{name}/stop`); the flow keeps receiving and queues them.

### The server stopped or crashed

Start it again. With a PostgreSQL store, every message the server acknowledged is stored, and
the server resumes queued and unfinished messages of started flows. Delivery is at least once: a
message being delivered when the server stopped may be delivered again, so receivers should
detect duplicates (HTTP destinations send the same `Idempotency-Key` on every attempt). Statistics
counted since the last sample are lost; messages are not.

### A bad change was deployed

Import the previous configuration export, or apply the previous config-as-code document:

```text
$ weavster -c ops.yaml
weavster> importcfg "weavster-config.json" force
```

Then reprocess the messages the bad change handled: `POST /api/v1/messages/{id}/reprocess` runs a
stored message through the flow again as a new message. See
[Work with one message](processing-messages.md#work-with-one-message).

### The database is lost

Restore the last [backup](#restore-the-database) into a new database, point `store.dsn` (or
`store.dsnEnv`) at it, and start the server. Messages received since that backup are gone,
unless their senders can send them again.
