# API errors

Every error reply of the REST API has the same JSON shape, with `Content-Type: application/json`:

```json
{"error": {"code": "NOT_FOUND", "message": "flow not found"}}
```

- `code` is stable and meant for programs; `message` is for people and can change.
- The HTTP status says what kind of error it is. Unknown endpoints and wrong methods get the same
  envelope as every other error.
- An unexpected server-side failure is `500` with the message `internal error`. The reason is
  never included in the reply, so it cannot leak internal details.

## Codes

| Status | `code` | Meaning |
|---|---|---|
| `400` | `BAD_REQUEST` | The request is invalid: malformed JSON, a definition that does not match the schema, a bad query parameter, or a missing `X-Weavster-CSRF` header. `message` names the problem. |
| `400` | `OLD_PASSWORD_INCORRECT`, `PASSWORD_REJECTED` | `POST /api/v1/auth/password`: the old password is wrong, or the new one breaks the password policy. |
| `401` | `UNAUTHORIZED` | Missing or wrong credentials, or a locked account. See [Authentication](authentication.md). |
| `403` | `FORBIDDEN` | The account lacks the permission the endpoint needs. |
| `403` | `PASSWORD_CHANGE_REQUIRED` | The account must change its password before doing anything else. |
| `404` | `NOT_FOUND` | Unknown flow, destination, action, or endpoint. |
| `405` | `METHOD_NOT_ALLOWED` | The endpoint exists but not with this method (`TRACE` is always refused). |
| `409` | `CONFLICT` | The request conflicts with the current state: the flow already exists, a lifecycle transition is not allowed from the flow's status, other flows depend on it, or an import would replace existing flows. |
| `413` | `PAYLOAD_TOO_LARGE` | The body is larger than the endpoint allows (10 MiB for messages, 50 MiB for import and bulk update). |
| `500` | `INTERNAL` | Unexpected server-side failure. |
| `500` | `IMPORT_INCOMPLETE`, `UPDATE_INCOMPLETE`, `REDEPLOY_INCOMPLETE` | A multi-flow operation stopped part-way; the reply also lists what was already written. |
| `501` | `NOT_IMPLEMENTED` | The feature exists only in the Enterprise edition. `message` names it, for example `not implemented in this edition: SSO`. |
| `503` | `SERVICE_UNAVAILABLE` | The part of the server that handles the request is not running, for example message endpoints with `store.dialect: disabled`. |

## In the command-line client

The [command-line client](cli.md) prints the status and `message`, and the command fails with exit
code `2`:

```text
Error: server returned 404 Not Found: flow not found
Error: server returned 501 Not Implemented: not implemented in this edition: SSO
```
