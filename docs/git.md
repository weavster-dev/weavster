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

## Compare revisions

`GET /api/v1/git/diff?from=REV[&to=REV]` lists the files changed between two revisions (`to`
defaults to `HEAD`) and the unified patch:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/git/diff?from=HEAD~2'
```

```json
{"from":"HEAD~2","to":"HEAD","truncated":false,"files":[{"path":"scripts/deploy.yaml","status":"modified"},{"path":"settings/retention.yaml","status":"deleted"}],
 "patch":"diff --git a/scripts/deploy.yaml b/scripts/deploy.yaml\n...\n-    deploy: log()\n+    deploy: log(2)\n..."}
```

- A moved or renamed file shows as `deleted` at the old path and `added` at the new one.
- A patch over 5 MiB is cut there and `truncated` is `true`; the file list is always complete.
- Without `from`, it lists the files in the repository directory that differ from `HEAD` but are
  not committed (`added`, `modified`, `deleted`; ignored files are not listed), with `from`
  `HEAD`, `to` empty, and no patch. Changes made through the API are always committed, so this
  shows only files edited on disk.

## Restore a revision

`POST /api/v1/git/restore` makes the repository match a revision and records that as a **new
commit**, so the history is kept. With `path`, only that file is restored:

```bash
# One file, as it was two commits ago
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1:8080/api/v1/git/restore \
  -d '{"rev":"HEAD~2","path":"scripts/deploy.yaml","message":"Put the deploy script back"}'
# Everything, as it was at a commit
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1:8080/api/v1/git/restore -d '{"rev":"5f0c1e9a8d","message":"Back to Monday"}'
```

```json
{"committed":true,"head":"9d4b7a0c2f...","changed":["scripts/deploy.yaml"]}
```

- A restore changes only the repository. To bring the server back to it,
  [check the drift](#check-for-drift) and [apply from the repository](#plan-and-apply-from-the-repository):
  every live change still goes through a plan you can review.
- Nothing to change: `committed` is `false` and no commit is made.
- An unknown revision, or a file that did not exist at it, returns `404`. Uncommitted files in the
  repository directory block a restore with `409`; commit or remove them first.
- A restore writes only the files that differ. Restoring the whole repository removes committed
  files the revision did not have (including ones outside the configuration directories); files
  that were never committed, and ignored files, are not touched.

## Check for drift

Drift is a difference between the live configuration and the repository: someone changed the
server after the last commit, or the repository was edited. `GET /api/v1/git/drift` compares the
two without changing anything (`rev` defaults to `HEAD`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/git/drift?rev=HEAD'
```

```json
{"rev":"HEAD","commit":"5f0c1e9a8d...","drifted":true,"plan":{"fingerprint":"9c1d...","added":["settings/retention"],"updated":["script/deploy"],"removed":["flow/tmp"],"unchanged":4,"changes":[...],"text":"..."}}
```

`commit` is the commit `rev` resolved to. `plan` is what applying the repository would do:
`added` exists only in the repository, `removed` only on the server; its `fingerprint` is the one
`config/apply?gitRev=` needs.

From the CLI, `config drift [revision]` prints the differences:

```text
weavster> config drift
~ script/deploy
    value: "log(2)" → "log()"
...
Error: the live configuration differs from the repository at HEAD (5f0c1e9a8d12): 3 changes
```

With no drift it prints `no drift: the live configuration matches the repository at HEAD (5f0c1e9a8d12)`.
In a script (`weavster -s`), drift makes the exit code `1` and a failed check (unknown revision,
server unreachable) `2`, so a CI job can tell them apart:

```bash
weavster -a https://weavster.example.com -u ci -p "$PASSWORD" -s <(echo 'config drift')
case $? in 0) echo "in sync";; 1) echo "drift";; *) echo "check failed"; exit 2;; esac
```

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
- A file that is not a valid config document, uses YAML anchors, aliases, or merge keys
  (`&x`, `*x`, `<<:`), or defines an artifact another file also defines returns `400` naming the
  file. An unknown revision returns `404`.
- The audit record of a plan or apply from the repository names the revision and the commit it
  resolved to (`git.rev`, `git.commit`).

## Share through a remote

Set [`git.remote`](server-config.md#git) to push the repository to a remote (GitHub, GitLab, a
bare repository on disk) and pull changes others pushed:

```yaml
git:
  path: /var/lib/weavster/config-repo
  remote:
    url: https://github.com/example/weavster-config.git
    username: x-access-token
    passwordEnv: WEAVSTER_GIT_TOKEN   # the token itself is in this environment variable
```

The server uses the remote's branch of the same name as its own (`main`).

Compare with the remote (it is fetched first; permission `git:view`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/git/remote
```

```json
{"url":"https://github.com/example/weavster-config.git","branch":"main","head":"5f0c1e9a8d...","remoteHead":"77b2d0c41e...","ahead":1,"behind":0}
```

`ahead` counts your commits the remote does not have; `behind` the remote's commits you do not
have. `remoteHead` is empty until the branch exists on the remote.

Push (permission `git:commit`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/git/push
```

It replies with the comparison after the push. When the remote has commits you do not have, the
push is refused with `409` (`… pull first`).

Pull (permission `git:commit`): the **remote wins**. The branch and the repository's files
become exactly the remote's; your commits that are not on the remote are dropped and listed:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/git/pull
```

```json
{"head":"77b2d0c41e...","dropped":["5f0c1e9a8d..."]}
```

- Push before you pull if you want to keep your commits; a dropped commit cannot be pulled back.
- A pull changes only the repository, never the live configuration. Check
  [drift](#check-for-drift), then [apply](#plan-and-apply-from-the-repository) to take the
  pulled configuration, or commit to record the live one on top.
- Without `git.remote.url` these endpoints answer `409` (`no remote configured`), as they do when
  the repository's HEAD is not on a branch. A remote that cannot be reached, refuses the
  credentials, or does not answer within a minute gives `502` with the reason; the password or
  token is never shown.
- A branch deleted on the remote stops being reported at the next status, push, or pull.
- A pull is refused with `409` while the repository has uncommitted files (they would be
  overwritten); commit or remove them first. If a pull fails part way, the branch and files are
  put back as they were committed.
- Checking the status fetches from the remote with the server's credentials; like `git fetch`,
  it updates only the repository's record of the remote.

## Permissions

| Operation | Permissions |
|---|---|
| `GET /api/v1/git`, `GET /api/v1/git/log`, `GET /api/v1/git/remote` | `git:view` |
| `POST /api/v1/git/push`, `POST /api/v1/git/pull` | `git:commit` |
| `GET /api/v1/git/drift` | `git:view` and the [config plan](config-as-code.md#see-what-would-change) permissions |
| `POST /api/v1/config/plan?gitRev=`, `POST /api/v1/config/apply?gitRev=` | `git:view` and that endpoint's own permissions |
| `GET /api/v1/git/content`, `GET /api/v1/git/diff` | `git:view`, `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit` |
| `POST /api/v1/git/restore` | `git:commit`, `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit` |
| `POST /api/v1/git/commit` | `git:commit`, `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit` |

File contents show the whole configuration, so reading them needs the same permissions as a
configuration export.
