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
| `detail` | `status` (HTTP status returned) plus `query.<name>` for each query parameter. Repeated parameters are joined with `,`. |

## What is recorded

| Event | `action` |
|---|---|
| Any `POST`, `PUT`, `PATCH`, or `DELETE` under `/api/v1` by a signed-in user, **including rejected ones** (for example `403 FORBIDDEN` or `403 PASSWORD_CHANGE_REQUIRED`) | method and route, e.g. `DELETE /api/v1/flows/{id}` |
| `GET /api/v1/messages` (message content can hold protected health information), including rejected reads | `phi.access` |
| Every `POST /api/v1/auth/login`, with the outcome in `status`: `200`, `400` (malformed body), `401`, `500`, or `503` | `auth.login` |
| Any other request rejected with `401`: missing credentials, wrong Basic password, or an unknown, expired, or revoked token | `auth.failure` |

Other reads, such as `GET /api/v1/flows` and `GET /api/v1/topology`, are not recorded.

## Redaction

Request bodies are never recorded, so passwords sent to `/auth/login` or `/auth/password`
never reach the log. A query parameter is logged as `[redacted]` when any word in its name is `password`, `token`,
`secret`, `authorization`, `credential`, `ssn`, or `phi`, ignoring case. A new word starts at
`-`, `_`, `.`, or a lowercase-to-uppercase change. So `apiToken`, `X-Token`, `client_secret`,
and `newPassword` are redacted, while `className` is not.

```text
actor=admin action=phi.access resource=/api/v1/messages detail="map[query.apiToken:[redacted] query.status:sent status:200]"
```

## Retention

Audit entries exist only in the log output and in process memory, which keeps up to the
newest 10,000 entries. They are not stored and cannot be searched through the API yet. To keep them, collect stderr with your log shipper
(for example journald or a container log driver).
