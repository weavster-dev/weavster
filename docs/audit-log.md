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
| `detail` | `status` (HTTP status returned) plus `query.<name>` for each query parameter. |

## What is recorded

| Event | `action` |
|---|---|
| Any `POST`, `PUT`, `PATCH`, or `DELETE` under `/api/v1` after authentication, **including rejected ones** (for example `403`) | method and route, e.g. `DELETE /api/v1/flows/{id}` |
| `GET /api/v1/messages` (message content can hold protected health information) | `phi.access` |
| `POST /api/v1/auth/login`, successful or not | `auth.login` |
| A request whose Basic credentials are rejected | `auth.failure` |

Other reads, such as `GET /api/v1/flows` and `GET /api/v1/topology`, are not recorded.

## Redaction

Request bodies are never recorded, so passwords sent to `/auth/login` or `/auth/password`
never reach the log. A query parameter whose name contains `password`, `token`, `secret`,
`authorization`, `credential`, `ssn`, or `phi` is logged as `[redacted]`. The match ignores
case and also covers partial names: `apiToken`, `X-Token`, `client_secret`.

```text
actor=admin action=phi.access resource=/api/v1/messages detail="map[query.apiToken:[redacted] query.status:sent status:200]"
```

## Retention

Audit entries exist only in the log output and in process memory. They are not stored and
cannot be searched through the API yet. To keep them, collect stderr with your log shipper
(for example journald or a container log driver).
