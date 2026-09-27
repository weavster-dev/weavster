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

### Where users are stored

Users, password changes, and lockout state are saved in the configured
[store](server-config.md#store):

| `store.dialect` | Users after a restart |
|---|---|
| `sqlite` | Kept. The first-start step runs only once per database. After that, the bootstrap variables are ignored and the printed password is never shown again. |
| `memory`, `disabled` | Lost. Every start repeats the first-start step, so a generated password changes each time. To keep a stable password, set `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD` or `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE`. |

If you lose a generated password on a `sqlite` store, there is no API to reset it. Stop the
server and delete the database file (this also deletes flows and messages), or keep the
password somewhere safe when it is first printed.

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
| `500 {"error":{"code":"INTERNAL",…}}` | The new password could not be saved (for example, the database is read-only), or another password change for the same user happened at the same moment. The old password still works; retry. |

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
    second account to unlock it. With `store.dialect: sqlite` the lockout also survives a
    restart, so you have to wait `lockoutPeriodSeconds`. Keep the server off untrusted
    networks. Setting `retryLimit: 0` removes this risk but allows unlimited password guessing.

## Permissions

| Route | Required permission |
|---|---|
| `GET /api/v1/system`, `/api/v1/auth/me`, `POST /api/v1/auth/password`, `POST /api/v1/auth/logout` | any signed-in user |
| `GET /api/v1/flows`, `GET /api/v1/flows/{id}`, `GET /api/v1/flows/export`, `GET /api/v1/flows/connector-names`, `GET /api/v1/flows/ports-in-use`, `GET /api/v1/topology`, `GET /api/v1/topology/flows/{flowId}` | `flows:view` |
| `POST /api/v1/flows`, `PUT /api/v1/flows`, `PUT /api/v1/flows/{id}`, `DELETE /api/v1/flows/{id}`, `POST /api/v1/flows/{id}/{enable,disable}`, `POST /api/v1/flows/import` | `flows:edit` |
| `GET /api/v1/messages`, `GET /api/v1/messages/{id}` | `messages:view` |
| `GET /api/v1/messages/{id}/content`, `GET /api/v1/messages/export` | `messages:content` |
| `POST /api/v1/messages/import` | `messages:import` |
| `DELETE /api/v1/messages/{id}`, `DELETE /api/v1/messages` | `messages:delete` |
| `POST /api/v1/flows/{id}/messages`, `POST /api/v1/messages/{id}/reprocess` | `messages:send` |
| `POST /api/v1/flows/{id}/{deploy,undeploy,start,stop,pause,halt,resume}`, `POST /api/v1/flows/redeploy-all`, `POST /api/v1/flows/{deploy,undeploy,start,stop,pause,halt,resume}-all`, `POST /api/v1/flows/{id}/destinations/{name}/{start,stop}`, `POST /api/v1/flows/stats/reset`, `POST /api/v1/flows/{id}/stats/reset` | `flows:deploy` |
| `GET /api/v1/flows/{id}/stats`, `GET /api/v1/flows/stats` | `flows:view` |
| `GET /api/v1/events` | `events:view` |
| `GET/POST /api/v1/users`, `GET/PUT/DELETE /api/v1/users/{name}`, `POST /api/v1/users/{name}/password` | `users:admin` |
| `/api/v1/configmap`, `/api/v1/configmap/{name}` (all methods) | `configmap:edit` |
| `/api/v1/scripts`, `/api/v1/scripts/{name}` (all methods) | `scripts:edit` |
| `/api/v1/settings`, `/api/v1/settings/{name}` (all methods) | `settings:edit` |
| `/api/v1/snippets`, `/api/v1/snippet-libraries` and their `/{name}` routes (all methods) | `snippets:edit` |

The `admin` permission grants everything. A signed-in user without the permission gets:

```json
{"error":{"code":"FORBIDDEN","message":"missing permission flows:edit"}}
```

The other permissions are `users:admin`, `flows:view`, `flows:edit`, `flows:deploy`,
`messages:view`, `messages:send`, `messages:content`, `messages:delete`, `messages:import`,
`events:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `configmap:edit`, and
`settings:edit`.

## Manage users

An account with `users:admin` (or `admin`) manages the other accounts.

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/users -d '{
  "username": "ops", "password": "A-Temp-Passw0rd",
  "permissions": ["flows:view", "flows:deploy", "events:view"],
  "email": "ops@example.com"
}'
```

```json
{"username":"ops","email":"ops@example.com","permissions":["events:view","flows:deploy","flows:view"],"mustChangePassword":true,"locked":false}
```

| Request | What it does |
|---|---|
| `GET /api/v1/users`, `GET /api/v1/users/{name}` | Lists accounts, or shows one. Password data is never returned. `locked` is true during a lockout. |
| `POST /api/v1/users` | Creates an account (`201`). `username` is 1–64 characters from `A-Z a-z 0-9 . _ @ -`; `permissions` must be from the list above; the [password policy](server-config.md#auth) applies. The user must choose a new password at the first login unless you send `"mustChangePassword": false`. |
| `PUT /api/v1/users/{name}` | Replaces `permissions` (required; `[]` for none). `email` and `org` change only when you send them. |
| `POST /api/v1/users/{name}/password` | Sets a new password (`{"password":"…"}`, `204`). The user must change it at the next login, and a lockout ends. |
| `DELETE /api/v1/users/{name}` | Deletes the account (`204`). |

- Changing a user's permissions, setting their password, or deleting them ends all their open
  sessions (bearer tokens); they sign in again.
- You cannot delete your own account, and the last account with `admin` cannot be deleted or
  lose `admin` (`409`).
- An account with `users:admin` but not `admin` can only grant permissions it holds itself, never
  `admin`, and cannot change, reset, or delete an account that has `admin` (`403`).
- Editing your own account (`PUT`) keeps your own session; setting your own password ends every
  session, yours included.
- Invalid input returns `400` with the reason, an existing username `409`, and an unknown user
  `404`.

## CLI

Batch mode sends `-u`/`-p` as Basic credentials:

```bash
weavster -a http://127.0.0.1:8080 -u admin -p 'A-Strong-Passw0rd' -s script.txt
```

Without them, `flow list` fails with exit code `2` and prints the server's reply:

```text
Error: server returned 401 Unauthorized: authentication required
```
