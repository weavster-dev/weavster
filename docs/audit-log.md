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

## Retention

Audit entries exist only in the log output and in process memory, which keeps exactly the
newest 10,000 entries. They are not stored and cannot be searched through the API yet. To keep them, collect stderr with your log shipper
(for example journald or a container log driver).
