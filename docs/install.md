# Install, upgrade, and roll back

Weavster is one static binary: the server, the command-line client, and `weavster test` in a
single file, with no runtime to install. Releases are published on
[GitHub releases](https://github.com/weavster-dev/weavster/releases) for three platforms:

| Platform | Archive |
|---|---|
| Linux, x86-64 | `weavster_<version>_linux_amd64.tar.gz` |
| Linux, ARM64 | `weavster_<version>_linux_arm64.tar.gz` |
| macOS, Apple silicon | `weavster_<version>_darwin_arm64.tar.gz` |

Each archive holds `weavster`, `LICENSE`, and `README.md` in a directory of the archive's name.
`weavster_<version>_checksums.txt` lists the SHA-256 checksum of every archive. Releases are not
signed yet; the checksum only proves the download is the file the release published.

## Install

```bash
version=1.2.0
os=linux arch=amd64          # or linux arm64, darwin arm64
base=https://github.com/weavster-dev/weavster/releases/download/v$version
curl -fsSLO "$base/weavster_${version}_${os}_${arch}.tar.gz"
curl -fsSLO "$base/weavster_${version}_checksums.txt"
sha256sum -c --ignore-missing "weavster_${version}_checksums.txt"   # macOS: shasum -a 256 -c --ignore-missing …
tar -xzf "weavster_${version}_${os}_${arch}.tar.gz"
sudo install -m 0755 "weavster_${version}_${os}_${arch}/weavster" /usr/local/bin/weavster
weavster version
```

```text
weavster_1.2.0_linux_amd64.tar.gz: OK
weavster 1.2.0 (built 2026-10-01T12:00:00Z, go1.22.12, linux/amd64)
```

- Stop if `sha256sum` does not print `OK` for your archive: the download is damaged or not the
  published file.
- `weavster version` reports the binary itself and needs no server. (The shell's `version`
  command, and `-v`, ask a server for its version.)
- On macOS, a downloaded binary may be quarantined; `xattr -d com.apple.quarantine
  /usr/local/bin/weavster` clears it after you have checked the checksum.
- To build from source instead, check out the release tag and run
  `scripts/release.sh 1.2.0 dist` (Go 1.22 or later, no C compiler needed): it builds the same
  archives and checksums. A plain `go build ./cmd/weavster` works too but calls itself `0.1.0`,
  which is also the release the database records for each schema upgrade it applies; stamp the
  version with `go build -ldflags "-X main.version=1.2.0" -o weavster ./cmd/weavster`.

Then set up the server: [Production setup](production.md) for a real deployment, or
[Docker Compose](docker-compose.md) to try it locally.

## Run in a container

The repository's `Dockerfile` builds a small image (`gcr.io/distroless/static-debian12`) that
runs the binary as a non-root user. Build it from the release's tag, with its version:

```bash
git checkout v1.2.0
docker build --build-arg VERSION=1.2.0 --build-arg BUILD_DATE="$(date -u +%FT%TZ)" -t weavster:1.2.0 .
docker run -d --name weavster -p 8443:8443 \
  -v /etc/weavster:/etc/weavster:ro \
  -e WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/weavster-admin-password \
  -v /srv/weavster/secrets:/run/secrets:ro \
  weavster:1.2.0 server --config /etc/weavster/weavster-server.yaml
```

- **Configuration**: mount the server configuration (and TLS certificate and key) read-only,
  and give their paths in the file.
- **State**: keep everything the server stores in PostgreSQL (`store.dialect: postgres`). The
  container itself keeps nothing: it can be replaced at any time. With `store.dialect: memory`,
  flows, users, messages, and statistics are lost when the container stops.
- **Secrets**: a secret a flow or the store names (`dsnEnv`, `passwordEnv`, `store.dsnEnv`) is an
  environment variable or a file of that name in `/run/secrets`, where Docker and Kubernetes
  mount secrets (see [secrets](server-config.md#secrets)). The first admin password comes from
  `WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE`. Keep secrets out of the image and out of the
  configuration file.
- **Secret files must be readable by the container's user** (`nonroot`, uid 65532): owned by it
  with mode `0400` (`chown 65532 FILE && chmod 0400 FILE`), or group-readable by a group you add
  with `--group-add`. A secret file only root can read fails with `secret NAME: … permission
  denied`, and the server exits.
- **Files flows read and write**: a file source or destination directory must be a volume the
  container's user (`nonroot`, uid 65532) can read and write.

With Docker Compose, the same setup uses `secrets:` (mounted in `/run/secrets`) and volumes:

```yaml
services:
  weavster:
    image: weavster:1.2.0
    command: ["server", "--config", "/etc/weavster/weavster-server.yaml"]
    environment:
      WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE: /run/secrets/weavster-admin-password
    secrets: [weavster-admin-password, WEAVSTER_STORE_DSN, WEAVSTER_DB_WAREHOUSE]
    volumes:
      - /etc/weavster:/etc/weavster:ro
      - /srv/weavster/inbox:/var/lib/weavster/inbox
    ports: ["8443:8443"]
secrets:
  weavster-admin-password: {file: /srv/weavster/secrets/admin-password}
  WEAVSTER_STORE_DSN: {file: /srv/weavster/secrets/store-dsn}          # store.dsnEnv: WEAVSTER_STORE_DSN
  WEAVSTER_DB_WAREHOUSE: {file: /srv/weavster/secrets/warehouse-dsn}   # a flow's dsnEnv
```

## Upgrade

1. Read the release notes for changes that affect you.
2. Back up the database, for example `pg_dump -Fc -f weavster-before-1.3.0.dump weavster`. The
   backup is what makes a rollback possible.
3. Stop the server (it drains in-flight messages, up to `listen.shutdownTimeoutMs`).
4. Replace the binary (or the image tag) and start the server.

At start, the server upgrades the database schema before it accepts any request. Each schema
change runs in its own transaction, so a failed upgrade leaves the database at the last completed
version, and servers starting together on one database take turns. Some upgrades lock a table
while they rebuild its indexes. See [Upgrades](server-config.md#upgrades) for the details.

## Roll back

Stop the new release and start the previous one:

- If the new release changed nothing in the database schema, the previous binary starts on the
  same database, and you are done.
- If it did, the previous binary refuses the database and exits `1` before changing anything:

  ```text
  Error: store: state: the database schema is at version 16 (written by weavster 1.3.0), newer than this release supports (15): run weavster 1.3.0 or later, or restore a backup taken before the upgrade
  ```

  Restore the backup taken before the upgrade (`pg_restore --clean -d weavster
  weavster-before-1.3.0.dump`), then start the previous release. Messages received after the
  upgrade are not in that backup: export them first if you need them (`exportmessages`).

## Common pitfalls

- **`sha256sum: … no properly formatted checksum lines`**: download the checksums file of the
  same version as the archive.
- **`permission denied` on a file source or a secret in a container**: the directory (or secret
  file) must be readable, and for a file source writable, by uid 65532, the image's non-root user.
- **`weavster version` says `0.1.0` in an image you built**: pass `--build-arg VERSION=…` to
  `docker build`.
- **The server refuses to start as root**: it runs as root only with `WEAVSTER_ALLOW_ROOT=1`.
  Run it as its own user instead.
