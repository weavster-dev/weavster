# Audit log

The server writes an audit entry for each security-relevant API call. Each entry is one
structured `audit` line on stderr:

```text
time=2026-09-26T12:00:00Z level=INFO msg=audit id=3 actor=admin action="POST /api/v1/flows" resource=/api/v1/flows detail=map[status:201]
```

| Field | Meaning |
|---|---|
| `actor` | Username from the request's credentials. For login and failed credentials, it is the username that was attempted. |
| `action` | See the table below. |
| `resource` | Request path. |
| `detail` | `status` (HTTP status returned) plus `query.<name>` for each query parameter. Repeated parameters are joined with `,`. `phi.access` entries also say what was disclosed; see below. |

## What is recorded

| Event | `action` |
|---|---|
| Any `POST`, `PUT`, `PATCH`, or `DELETE` under `/api/v1` by a signed-in user, **including rejected ones** (for example `403 FORBIDDEN` or `403 PASSWORD_CHANGE_REQUIRED`) | method and route, e.g. `DELETE /api/v1/flows/{id}` |
| `GET /api/v1/messages`, `GET /api/v1/messages/{id}`, `GET /api/v1/messages/{id}/content`, and `GET /api/v1/messages/export` (message content can hold protected health information), including rejected reads | `phi.access` |
| Every `POST /api/v1/auth/login`, with the outcome in `status`: `200`, `400` (malformed body), `401`, `500`, or `503` | `auth.login` |
| Any other request rejected with `401`: missing credentials, wrong Basic password, or an unknown, expired, or revoked token | `auth.failure` |
| A non-GET request (including login) rejected with `400` for a missing `X-Weavster-CSRF` marker | method and literal path, e.g. `POST /api/v1/auth/login` |

Other reads, such as `GET /api/v1/flows` and `GET /api/v1/topology`, are not recorded.

### What a `phi.access` entry discloses

| Request | Added to `detail` |
|---|---|
| `GET /api/v1/messages` (search) | `messages`: how many messages were returned; `messages.ids`: every returned id, as a JSON array (up to `limit`, at most 1000). |
| `GET /api/v1/messages/export` | `messages` and `messages.ids` for every message in the archive (up to 10,000, so this entry can be long; allow for it in your log shipper's line limit). |
| `GET /api/v1/messages/{id}` | The message id is the `resource`. |
| `GET /api/v1/messages/{id}/content` | `part`: the content form returned, `raw` when the request gave none. |
| `POST /api/v1/messages/{id}/requeue` | `message.id`, and `previous.<destination>.attempts`: the attempts each destination had before the requeue. |

A read that was refused or failed (for example `403` or `404`) adds none of these: its entry has
`status` and the `query.<name>` parameters only, since nothing was disclosed.

```text
actor=admin action=phi.access resource=/api/v1/messages detail="map[messages:1 messages.ids:[\"6f1c9e\"] query.flowId:adt status:200]"
actor=admin action=phi.access resource=/api/v1/messages/6f1c9e/content detail="map[part:raw status:200]"
```

## Redaction

Request bodies are never recorded, so passwords sent to `/auth/login` or `/auth/password`
never reach the log. A query parameter is logged as `[redacted]` when, ignoring case:

- its name contains `password`, `token`, `secret`, `authorization`, or `credential` anywhere
  (`apiToken`, `X-Token`, `client_secret`, `newPassword`, `notpassword`), or
- one of its words is `ssn` or `phi` (`patient_ssn`, `phiFlag`). A new word starts at `-`,
  `_`, `.`, or a lowercase-to-uppercase change, so `className` and `graphId` are not redacted.

```text
actor=admin action=phi.access resource=/api/v1/messages detail="map[messages:0 messages.ids:[] query.apiToken:[redacted] query.status:sent status:200]"
```

## Search the audit log

Every entry is also stored in the server's [store](server-config.md#store), redacted exactly as it
is logged, and the `id` on the stderr line is the stored entry's `id`. With `store.dialect: postgres` it survives restarts; with `memory` or `disabled` the
newest 100,000 entries are kept until the server stops. Search them with `GET /api/v1/audit`
(permission `audit:view`, which `admin` includes):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/audit?action=phi.access&from=2026-09-28T00:00:00Z&limit=100'
```

```json
[{"id":412,"at":"2026-09-28T12:03:11.204Z","actor":"alice","action":"phi.access",
  "resource":"/api/v1/messages","detail":{"messages":"2","messages.ids":"[\"6f1c9e\",\"9a0d41\"]","query.flowId":"adt","status":"200"}}]
```

| Parameter | Meaning |
|---|---|
| `actor`, `action`, `resource` | Only entries with exactly this actor, action (for example `phi.access`, `auth.failure`, `POST /api/v1/flows`), or resource (the request path). |
| `from`, `to` | At or after / at or before this time (RFC 3339). |
| `afterId` | Only entries with a larger `id`: page with the last `id` you saw. |
| `limit` | Entries per page, 1–1000 (default 100). Entries come oldest first. |

An entry shows up in the search about 3 seconds after it was written. By then every earlier entry
is stored too, so a collector that polls with `afterId` never skips one. Without an `action`
filter, entries of reads of the audit log itself (`audit.read`) are left out, so a collector does
not keep finding its own polls; ask for them with `action=audit.read`.

Reading the audit log is itself recorded, as `audit.read`, with the number of entries returned in
`entries`; a search that returns nothing discloses nothing and is not recorded. If the store cannot take an entry (for example the database is down), the entry still
goes to stderr, the server logs `audit entry not stored`, and the request itself is not affected. A
store write waits at most 2 seconds.

## Retention

Stored entries are kept until they are pruned: set
[`prune.auditMaxAgeDays`](server-config.md#prune) to remove entries older than that many days
(each prune pass reports how many in `lastRun.auditRemoved`). Without it they are kept forever,
and every failed login adds one. To also keep them outside the server, collect stderr with your
log shipper (for example journald or a container log driver).
