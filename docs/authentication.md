# Authentication

Every `/api/v1` route needs credentials, except `POST /api/v1/auth/login`.
`GET /api/openapi.yaml` stays public. You can authenticate in either of two ways:

- **HTTP Basic** on each request: `curl -u admin:PASSWORD …`
- **Bearer token** from `POST /api/v1/auth/login`: `-H "Authorization: Bearer TOKEN"`

A request without valid credentials gets:

```text
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Basic realm="weavster"

{"error":{"code":"UNAUTHORIZED","message":"authentication required"}}
```

## First start: the admin account

When the server starts with no users, it creates one account named `admin` with every
permission. It picks the password in this order:

1. The `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD` environment variable.
2. The contents of the file named by `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE`, for example a
   container secret at `/run/secrets/weavster-admin`. A trailing newline is ignored.
3. Otherwise, a random 24-character password, printed **once** to stderr:

```text
First-run admin account created.
  username: admin
  password: q7M!xR...
This password is shown once and must be changed at first login (POST /api/v1/auth/password).
```

The password from option 1 or 2 must satisfy [`auth.passwordPolicy`](server-config.md#auth).
If it doesn't, the server exits `1` with `Error: bootstrap: admin password rejected by auth.passwordPolicy`.

!!! warning "Users are not persisted yet"
    Users live in memory. Every restart repeats the first-start step, so a generated password
    changes on every start and a changed password is lost. For a stable password, set
    `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD` or `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE`.

### Changing a generated password

Until you change a generated password, every route except `/api/v1/auth/me`,
`/api/v1/auth/password`, and `/api/v1/auth/logout` returns:

```json
{"error":{"code":"PASSWORD_CHANGE_REQUIRED","message":"change your password with POST /api/v1/auth/password before using the API"}}
```

Change it:

```bash
curl -s -u 'admin:q7M!xR...' -H 'X-Weavster-CSRF: 1' \
  -d '{"oldPassword":"q7M!xR...","newPassword":"A-Strong-Passw0rd"}' \
  http://127.0.0.1:8080/api/v1/auth/password
```

This returns `204` on success. Errors:

| Response | Cause |
|---|---|
| `400 {"error":{"code":"OLD_PASSWORD_INCORRECT",…}}` | `oldPassword` is wrong. It counts as a failed attempt toward lockout. |
| `400 {"error":{"code":"PASSWORD_REJECTED",…}}` | `newPassword` fails `auth.passwordPolicy` or equals `oldPassword`. |

A password change signs out every other session of that user. The token used for the change
keeps working.

## Log in, use a token, log out

```bash
curl -s -H 'X-Weavster-CSRF: 1' \
  -d '{"username":"admin","password":"A-Strong-Passw0rd"}' \
  http://127.0.0.1:8080/api/v1/auth/login
```

```json
{"expiresAt":"2026-09-27T04:00:00Z","token":"5f0c…","user":{"username":"admin","permissions":["admin"],"mustChangePassword":false}}
```

A token is valid for 12 hours, or until you log out, change your password from another
session, or the server restarts. The scheme name is case-insensitive (`bearer` works too).

```bash
curl -s -H 'X-Weavster-CSRF: 1' -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/auth/me
curl -s -X POST -H 'X-Weavster-CSRF: 1' -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/auth/logout   # 204
```

A wrong username and a wrong password both return the same
`401 {"error":{"code":"UNAUTHORIZED","message":"invalid username or password"}}`.

## Lockout

After `auth.lockout.retryLimit` failed attempts (default `5`), the account is locked for
`auth.lockout.lockoutPeriodSeconds` (default `300`). Failed attempts count on both the login
endpoint and Basic credentials. While the account is locked, even the correct password
returns `401`. Set `retryLimit: 0` to disable lockout. See [Server configuration](server-config.md#auth).

!!! warning "Lockout can shut out the only admin"
    Anyone who can reach the API can lock `admin` by sending wrong passwords, and there is no
    second account to unlock it. Restarting the server clears the lockout, but it also clears the
    in-memory flows. Keep the server off untrusted networks. Setting `retryLimit: 0` removes this
    risk but allows unlimited password guessing.

## Permissions

| Route | Required permission |
|---|---|
| `GET /api/v1/system`, `/api/v1/auth/me`, `POST /api/v1/auth/password`, `POST /api/v1/auth/logout` | any signed-in user |
| `GET /api/v1/flows`, `GET /api/v1/flows/{id}`, `GET /api/v1/topology`, `GET /api/v1/topology/flows/{flowId}` | `flows:view` |
| `POST /api/v1/flows`, `DELETE /api/v1/flows/{id}` | `flows:edit` |
| `GET /api/v1/messages` | `messages:view` |

The `admin` permission grants everything. A signed-in user without the permission gets:

```json
{"error":{"code":"FORBIDDEN","message":"missing permission flows:edit"}}
```

The API cannot create other users yet; the only account is `admin`.

## CLI

Batch mode sends `-u`/`-p` as Basic credentials:

```bash
weavster -a http://127.0.0.1:8080 -u admin -p 'A-Strong-Passw0rd' -s script.txt
```

Without them, `flow list` fails with exit code `2` and prints the server's reply:

```text
Error: server returned 401 Unauthorized: {"error":{"code":"UNAUTHORIZED","message":"authentication required"}}
```
