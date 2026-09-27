# Git repository

The server can keep its configuration in a Git repository: every commit records the flows,
alerts, snippets, snippet libraries, global scripts, and settings as they are live on the server,
so you can see what changed, when, and by whom, read any earlier version, check whether the
server has drifted from it, and apply it back.

## Turn it on

Set `git.path` in the [server configuration](server-config.md#git):

```yaml
git:
  path: /var/lib/weavster/config-repo
```

The directory is created with an empty repository on branch `main` when it does not exist; an
existing repository is used as it is. Without `git.path`, the endpoints below answer `503`.

## Commit the live configuration

`POST /api/v1/git/commit` writes the live configuration into the repository and commits it,
with you as the author. It needs `git:commit` and the permissions of a
[configuration export](config-transfer.md) (`flows:view`, `alerts:edit`, `snippets:edit`,
`scripts:edit`, `settings:edit`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1:8080/api/v1/git/commit -d '{"message":"Add the ADT feed"}'
```

```json
{"committed":true,"head":"5f0c1e9a8d...","changed":["flows/adt.yaml","scripts/deploy.yaml"]}
```

- `changed` lists the files added, updated, or removed. When the repository already matches the
  live configuration nothing is committed: `committed` is `false`, `changed` is empty, and `head`
  is the current commit.
- `message` is required.
- Commits run one at a time. Each artifact is read once while the commit runs, so a change made
  during a commit may show up only in the next one.
- Only `.yaml` files directly in the directories below are managed. Other files (a `README.md`,
  `flows/examples/demo.yaml`) are kept, and committed along with the configuration when they
  changed.
- If a commit fails, the files it touched are put back and nothing is staged.

### Repository layout

Each artifact is one [config-as-code document](config-as-code.md) holding just that artifact:

| Artifact | File |
|---|---|
| Flow `adt` | `flows/adt.yaml` |
| Alert `errors` | `alerts/errors.yaml` |
| Snippet `pid` | `snippets/pid.yaml` |
| Snippet library `hl7` | `snippetLibraries/hl7.yaml` |
| Global script `deploy` | `scripts/deploy.yaml` |
| Setting `retention` | `settings/retention.yaml` |

For example, `scripts/deploy.yaml`:

```yaml
version: "1"
scripts:
    deploy: log()
```

- The file name is the artifact's name (names use only letters, digits, `.`, `_`, and `-`).
- Two artifacts of a kind whose names differ only in case (`ADT` and `adt`) would share a file on
  macOS and Windows, so the commit is refused with `409` naming both; rename one.
- A deleted artifact's file is removed by the next commit.
- The config map is never committed: it holds environment-specific values.
- A file can refer to an artifact in another file (a snippet names its library), so validate the
  files together, not one by one.

## Read the history

Repository information and the log need `git:view`:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/git
```

```json
{"branch":"main","head":"5f0c1e9a8d..."}
```

`head` is empty before the first commit.

The log, newest first (`limit` 1–1000, default 100); with `path`, only the commits that changed
that file:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/git/log?path=flows/adt.yaml&limit=10'
```

```json
[{"hash":"5f0c1e9a8d...","message":"Add the ADT feed","author":"admin","at":"2026-09-27T10:00:00Z"}]
```

A file as it was at a revision (`git:view` plus the export permissions above). `rev` is anything
Git understands: a full or short hash, `HEAD~1`, a branch; it defaults to `HEAD`. The file comes
back as stored: `application/yaml` for `.yaml` files, otherwise the type of its content:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/git/content?path=flows/adt.yaml&rev=HEAD~1'
```

An unknown revision, or a file that did not exist at it, returns `404`.

## Check for drift

Drift is a difference between the live configuration and the repository: someone changed the
server after the last commit, or the repository was edited. `GET /api/v1/git/drift` compares the
two without changing anything (`rev` defaults to `HEAD`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/git/drift?rev=HEAD'
```

```json
{"rev":"HEAD","drifted":true,"plan":{"fingerprint":"9c1d...","added":["settings/retention"],"updated":["script/deploy"],"removed":["flow/tmp"],"unchanged":4,"changes":[...],"text":"..."}}
```

`plan` is what applying the repository would do: `added` exists only in the repository,
`removed` only on the server. From the CLI, `config drift [revision]` prints the differences and
fails when there are any, so a CI job can gate on it:

```text
weavster> config drift
~ script/deploy
    value: "log(2)" → "log()"
...
Error: the live configuration differs from the repository at HEAD (3 changes)
```

With no drift it prints `no drift: the live configuration matches the repository at HEAD`.

Drift is only checked when you ask. Checking on a schedule and repairing drift automatically is
an Enterprise feature.

## Plan and apply from the repository

The [config plan and apply](config-as-code.md#see-what-would-change) endpoints take the
repository as their document when you pass `gitRev` (a revision; empty means `HEAD`) instead of
a body. This also needs `git:view`.

```bash
# What applying the repository at HEAD would change
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST 'http://127.0.0.1:8080/api/v1/config/plan?gitRev='
# Apply exactly that plan
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  'http://127.0.0.1:8080/api/v1/config/apply?gitRev=&fingerprint=9c1d...&reason=restore%20from%20git'
```

Rolling back to an earlier version is applying an earlier revision, for example `gitRev=HEAD~1`
or a commit hash.

How the repository becomes one document:

- Every `.yaml` file directly in `flows/`, `alerts/`, `snippets/`, `snippetLibraries/`,
  `scripts/`, and `settings/` is read and merged. A file may hold several artifacts, of any of
  these sections; other files are ignored.
- All six sections are managed: an artifact on the server that the repository lacks is removed
  by an apply.
- The config map is never read from the repository and is left as it is. A file with a
  `configmap` section is refused.
- A file that is not a valid config document, or an artifact defined in two files, returns `400`
  naming the file. An unknown revision returns `404`.

## Permissions

| Operation | Permissions |
|---|---|
| `GET /api/v1/git`, `GET /api/v1/git/log` | `git:view` |
| `GET /api/v1/git/drift` | `git:view` and the [config plan](config-as-code.md#see-what-would-change) permissions |
| `POST /api/v1/config/plan?gitRev=`, `POST /api/v1/config/apply?gitRev=` | `git:view` and that endpoint's own permissions |
| `GET /api/v1/git/content` | `git:view`, `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit` |
| `POST /api/v1/git/commit` | `git:commit`, `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit` |

File contents show the whole configuration, so reading them needs the same permissions as a
configuration export.
