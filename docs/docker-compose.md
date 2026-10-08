# Local development with Docker Compose

`docker-compose.yml` in the repository runs the server with PostgreSQL 16 on your machine. It is
for development and trying things out: its passwords are fixed and written in the repository,
and the database connection is not encrypted. For a real deployment, see
[Production setup](production.md).

## Start

From the repository root:

```bash
docker compose up -d --build --wait
```

This builds the server image from the `Dockerfile`, starts PostgreSQL, waits until the database
is ready, and starts the server with `docker/weavster.yaml`. `--wait` returns once the server's
container runs; the server connects to the database and answers a moment later (on the first
start, a few seconds). Wait for it, then use the API at `http://127.0.0.1:8080` (only on your
machine):

```bash
until curl -fsS http://127.0.0.1:8080/api/openapi.yaml >/dev/null; do sleep 1; done
curl -s -u 'admin:Weavster-dev-1' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/flows
```

```json
[]
```

| What | Value |
|---|---|
| API | `http://127.0.0.1:8080` |
| First-run admin | `admin` / `Weavster-dev-1` (from `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD` in the compose file) |
| Database | `postgres://weavster:weavster@db:5432/weavster` inside the compose network; not published on your machine |
| Server configuration | `docker/weavster.yaml`, mounted read-only at `/etc/weavster/weavster.yaml` |

## Stop, restart, reset

| Command | Effect |
|---|---|
| `docker compose logs -f weavster` | Follow the server's log. |
| `docker compose restart weavster` | Restart the server; flows, messages, and users are kept. |
| `docker compose up -d --build --wait` | Rebuild the server after a code change and restart it; the data is kept. |
| `docker compose down` | Stop and remove the containers; the data is kept in the `weavster_db-data` volume. |
| `docker compose down -v` | Stop and **delete all data** (the volume). The next start creates the admin account again. |

The server container restarts by itself if the server stops (for example, when the database
was not reachable in time); `docker compose ps` shows its state.

To change the server's settings, edit `docker/weavster.yaml` (see
[Server configuration](server-config.md)) and run `docker compose restart weavster`.

## Check the stack

`scripts/compose-smoke.sh` starts the stack, creates a flow, restarts the server, and checks the
flow is still there. CI runs it on every pull request:

```bash
scripts/compose-smoke.sh
```

```text
compose smoke test passed
```

## Common problems

- **Port 8080 is taken.** `docker compose up` fails with `port is already allocated`. Stop what uses
  the port, or change the left side of `"127.0.0.1:8080:8080"` in `docker-compose.yml` (for
  example `"127.0.0.1:9080:8080"`) and use that port in your requests.
- **The admin password changed and you lost it.** Run `docker compose down -v` to start over
  with the compose file's password (this deletes flows and messages too).
- **The server keeps restarting with `permission denied` in its log.** The server runs as an
  unprivileged user and reads `docker/weavster.yaml` through the mount, so the file must be
  readable by everyone on your machine: `chmod 644 docker/weavster.yaml` (a checkout with a
  strict `umask` can leave it `600`).
- **`400` on every request.** Add the `X-Weavster-CSRF: 1` header (see
  [`listen.requireMarkerHeader`](server-config.md#listen)).
- **Don't reuse these settings in production.** The passwords are public and `sslmode=disable`
  sends database traffic unencrypted.
