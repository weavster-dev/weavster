# Git repository

The server can keep its configuration in a Git repository: every commit records the flows,
alerts, snippets, snippet libraries, global scripts, and settings as they are live on the server,
so you can see what changed, when, and by whom, and read any earlier version.

## Turn it on

Set `git.path` in the [server configuration](server-config.md#git):

```yaml
git:
  path: /var/lib/weavster/config-repo
```

The directory is created with an empty repository on branch `main` when it does not exist; an
existing repository is used as it is. Without `git.path`, the endpoints below answer `503`.

## Commit the live configuration

`POST /api/v1/git/commit` (permission `git:commit`) writes the live configuration into the
repository and commits it, with you as the author:

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
- Other files in the repository (a `README.md`, for example) are kept, and committed along with
  the configuration when they changed.

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

- Names are URL-path-escaped in file names: a snippet `a/b` is `snippets/a%2Fb.yaml`.
- A deleted artifact's file is removed by the next commit.
- The config map is never committed: it holds environment-specific values.
- A file can refer to an artifact in another file (a snippet names its library), so validate the
  files together, not one by one.

## Read the history

Repository information (permission `git:view` for this and the rest of this section):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/git
```

```json
{"path":"/var/lib/weavster/config-repo","branch":"main","head":"5f0c1e9a8d..."}
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

A file as it was at a revision. `rev` is anything Git understands: a full or short hash,
`HEAD~1`, a branch; it defaults to `HEAD`. The file comes back as stored (`application/yaml`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/git/content?path=flows/adt.yaml&rev=HEAD~1'
```

An unknown revision, or a file that did not exist at it, returns `404`.

## Permissions

- `git:view` reads the repository, which includes every committed definition. Give it only to
  users who may see the whole configuration.
- `git:commit` commits the live configuration.
