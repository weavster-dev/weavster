# CI/CD

Keep the server's configuration in a repository as a [config-as-code document](config-as-code.md)
(`weavster.yaml`) and let CI do the rest:

- **On a pull request or merge request** the pipeline shows what applying the changed document
  would do to the server ([`config diff`](cli.md)). Nothing changes on the server. An invalid
  document, or a server that refuses the credentials, fails the job with the reason.
- **On merge to `main`** the pipeline applies the document ([`config apply`](config-as-code.md#apply))
  and then deploys the flows that are `enabled` but not yet deployed ([`deploy`](cli.md)) and
  starts them ([`flow start-all`](cli.md)), so the merged change takes effect. The
  apply plans again first, so it applies what the server needs at that moment, and records the
  commit in the [audit log](audit-log.md) as the reason. If a change fails, the apply undoes the
  changes it already made; if undoing one also fails, it reports `rollback failed` with what is
  left, and the job fails. Deploying and starting run only after a successful apply.

Copy a sample below into the repository that holds `weavster.yaml`; the files are also available
to download: [`github-actions.yml`](examples/ci/github-actions.yml),
[`gitlab-ci.yml`](examples/ci/gitlab-ci.yml), [`weavster.yaml`](examples/ci/weavster.yaml).

## Before you start

1. A server reachable from CI over **HTTPS** (`listen.tlsAddress`, see
   [Server configuration](server-config.md)). Plain HTTP would send the password in cleartext.
2. Two users for CI, so a pull request cannot apply (see the table below).
3. Secrets in the CI system: `WEAVSTER_ADDRESS` (for example `https://weavster.example.com`), and
   `WEAVSTER_USER` / `WEAVSTER_PASSWORD` once for each user, scoped as the sample's header
   comment describes: the plan user's values for every pipeline, the apply user's values only for
   the protected `production` environment. The jobs write them to a [connection file](cli.md)
   readable only by the job, so the password never appears on a command line or in the log.

| User | Used by | Permissions |
|---|---|---|
| Plan user | pull/merge request pipelines | `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit`, `configmap:edit` (the [config plan permissions](config-as-code.md#see-what-would-change)) |
| Apply user | merges to `main` only | the plan permissions plus `flows:edit` and `flows:deploy` (deploy and start) |

## The document

Only the sections the document has are managed; the server keeps what it has for the others. For
example, this document manages flows, scripts, and settings, and leaves alerts, snippets, and the
config map alone:

```yaml
# The configuration this repository manages (config-as-code, version 1).
# Sections left out are not managed: the server keeps what it has for them.
version: "1"
flows:
  adt:
    id: adt
    name: ADT inbound
    enabled: true
    destinations:
      - name: archive
        type: file
        dir: /var/lib/weavster/out/adt
scripts:
  deploy: log("deployed from CI")
settings:
  retention: {days: 30}
```

> **Warning: a managed section is managed completely.** Applying this document to a server that
> already has other flows, scripts, or settings **removes them**, because they are not in the
> document. Before the first merge, make the document list everything you want to keep, and read
> the pull request's plan: every `-` line is a removal.

## GitHub Actions

Save as `.github/workflows/weavster.yml`. The plan appears in the job summary of the pull
request's checks (and in the step log). Add required reviewers to the `production` environment to
have a person approve each apply.

```yaml
# Weavster config-as-code on GitHub Actions: plan on pull requests, apply on
# merge to main. Save as .github/workflows/weavster.yml in the repository that
# holds weavster.yaml.
#
# Secrets: WEAVSTER_ADDRESS (https://weavster.example.com) as a repository
# secret. WEAVSTER_USER / WEAVSTER_PASSWORD twice: as repository secrets for
# the plan user, and as secrets of the `production` environment for the apply
# user (environment secrets override repository secrets in the apply job).
# GitHub gives no secrets to pull requests from forks: their plan job fails to
# log in, and a maintainer runs the plan from a branch in this repository.
name: weavster-config

on:
  pull_request:
    paths: [weavster.yaml]
  push:
    branches: [main]
    paths: [weavster.yaml]

permissions:
  contents: read

env:
  WEAVSTER_VERSION: latest # pin a commit hash (or a release tag once releases exist)

jobs:
  plan:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    # A newer push to the same pull request replaces its running plan.
    concurrency:
      group: weavster-plan-${{ github.ref }}
      cancel-in-progress: true
    steps:
      - &checkout
        uses: actions/checkout@v4
      - &setup-go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - &install
        name: Install weavster
        run: go install "github.com/weavster-dev/weavster/cmd/weavster@${WEAVSTER_VERSION}"
      - &connect
        name: Write the connection file
        env:
          WEAVSTER_ADDRESS: ${{ secrets.WEAVSTER_ADDRESS }}
          WEAVSTER_USER: ${{ secrets.WEAVSTER_USER }}
          WEAVSTER_PASSWORD: ${{ secrets.WEAVSTER_PASSWORD }}
        run: |
          umask 077
          quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"; }
          {
            printf 'address: %s\n' "$(quote "$WEAVSTER_ADDRESS")"
            printf 'user: %s\n' "$(quote "$WEAVSTER_USER")"
            printf 'password: %s\n' "$(quote "$WEAVSTER_PASSWORD")"
          } > "$RUNNER_TEMP/weavster.conn"
      - name: Plan
        run: |
          printf 'config diff "weavster.yaml"\n' > "$RUNNER_TEMP/plan.txt"
          status=0
          weavster -c "$RUNNER_TEMP/weavster.conn" -s "$RUNNER_TEMP/plan.txt" > plan.out 2>&1 || status=$?
          cat plan.out
          {
            echo '### Weavster plan for weavster.yaml'
            echo '```'
            cat plan.out
            echo '```'
          } >> "$GITHUB_STEP_SUMMARY"
          exit "$status"

  apply:
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    environment: production # holds the apply user's secrets; add required reviewers to gate applies
    # One apply at a time. A newer merge replaces an apply still waiting to
    # start; it applies the whole document, so nothing is lost.
    concurrency:
      group: weavster-apply
      cancel-in-progress: false
    steps:
      - *checkout
      - *setup-go
      - *install
      - *connect
      - name: Apply, deploy, and start
        # Separate runs, so nothing is deployed or started if the apply fails.
        run: |
          printf 'config apply "weavster.yaml" %s\n' "merged ${GITHUB_SHA}" > "$RUNNER_TEMP/apply.txt"
          printf 'deploy 120\n' > "$RUNNER_TEMP/deploy.txt"
          printf 'flow start-all\n' > "$RUNNER_TEMP/start.txt"
          weavster -c "$RUNNER_TEMP/weavster.conn" -s "$RUNNER_TEMP/apply.txt" &&
            weavster -c "$RUNNER_TEMP/weavster.conn" -s "$RUNNER_TEMP/deploy.txt" &&
            weavster -c "$RUNNER_TEMP/weavster.conn" -s "$RUNNER_TEMP/start.txt"
```

## GitLab CI

Save as `.gitlab-ci.yml`. Protect `main`, so only merges can apply. The plan is in the merge
request pipeline's job log. Only pushes to `main` apply; scheduled, manual, and API pipelines do
not.

```yaml
# Weavster config-as-code on GitLab CI: plan on merge requests, apply on
# merge to main. Save as .gitlab-ci.yml in the repository that holds
# weavster.yaml.
#
# CI/CD variables: WEAVSTER_ADDRESS (https://weavster.example.com).
# WEAVSTER_USER / WEAVSTER_PASSWORD twice: masked for all environments for
# the plan user (not protected: merge request pipelines run on unprotected
# branches), and masked + protected for the `production` environment for the
# apply user (the environment-scoped values win in the apply job).
# Anyone who can push a branch to this project can read the plan user's
# password (a branch can change this file); merge requests from forks run in
# the fork without these variables.
#
# Set the resource group to run applies in order, once per project:
#   curl --request PUT --header "PRIVATE-TOKEN: $TOKEN" \
#     "https://gitlab.example.com/api/v4/projects/$PROJECT_ID/resource_groups/weavster-config?process_mode=oldest_first"
stages: [plan, apply]

variables:
  WEAVSTER_VERSION: latest # pin a commit hash (or a release tag once releases exist)

.weavster:
  image: golang:1.22
  before_script:
    - go install "github.com/weavster-dev/weavster/cmd/weavster@${WEAVSTER_VERSION}"
    - |
      umask 077
      quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"; }
      {
        printf 'address: %s\n' "$(quote "$WEAVSTER_ADDRESS")"
        printf 'user: %s\n' "$(quote "$WEAVSTER_USER")"
        printf 'password: %s\n' "$(quote "$WEAVSTER_PASSWORD")"
      } > /tmp/weavster.conn

plan:
  extends: .weavster
  stage: plan
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      changes: [weavster.yaml]
  script:
    - printf 'config diff "weavster.yaml"\n' > /tmp/plan.txt
    - weavster -c /tmp/weavster.conn -s /tmp/plan.txt

apply:
  extends: .weavster
  stage: apply
  resource_group: weavster-config # one apply at a time, oldest first (see above)
  environment: production
  rules:
    - if: $CI_PIPELINE_SOURCE == "push" && $CI_COMMIT_BRANCH == "main"
      changes: [weavster.yaml]
  script:
    # Separate runs, so nothing is deployed or started if the apply fails.
    - printf 'config apply "weavster.yaml" %s\n' "merged ${CI_COMMIT_SHORT_SHA}" > /tmp/apply.txt
    - printf 'deploy 120\n' > /tmp/deploy.txt
    - printf 'flow start-all\n' > /tmp/start.txt
    - weavster -c /tmp/weavster.conn -s /tmp/apply.txt
    - weavster -c /tmp/weavster.conn -s /tmp/deploy.txt
    - weavster -c /tmp/weavster.conn -s /tmp/start.txt
```

## Pitfalls

- **Pin the version.** `WEAVSTER_VERSION: latest` installs the newest client from the default
  branch. Pin the commit hash (or, once releases exist, the release tag) that matches your server.
- **Who can use the plan user.** The plan needs the same permissions as a configuration export,
  which include editing alerts, snippets, scripts, settings, and the config map through the API
  (there is no read-only plan permission yet); it cannot apply a document or change flows. Anyone
  who can push a branch to the repository can change the pipeline and read the plan user's
  password, so give push access only to people you would trust with those permissions. Pull and
  merge requests from forks get no secrets: their plan job cannot log in, and a maintainer runs the
  plan from a branch in the repository instead.
- **Order applies on GitLab.** A resource group runs one job at a time but not in order by
  default. Set its process mode to `oldest_first` once (the command is in the sample's header),
  or an older queued apply could run after a newer one and restore an older `weavster.yaml`.
- **The server moved on.** If someone changes the server between the plan and the merge, the
  apply job plans again and applies the current differences, which may be more than the pull
  request showed. Check the apply job's output; to be warned instead, run the pull request plan
  again just before merging.
- **Sections you leave out are not managed.** Deleting a whole section from `weavster.yaml` does
  not remove those artifacts from the server; keep the section with no entries (`scripts: {}`)
  to remove them all.
- **One apply at a time.** The samples queue applies (`concurrency`, `resource_group`). On GitHub a
  newer merge replaces an apply that has not started yet; the newer apply applies the whole
  document, so nothing is lost. Do not apply to one server from two pipelines at once.
- **`flow start-all` starts every deployed or stopped flow,** including one someone stopped on
  purpose. If you stop flows by hand, remove that line and start new flows yourself, or keep a
  flow off by setting `enabled: false` and undeploying it.
- **Secrets in the document.** `weavster.yaml` is committed: keep passwords and tokens out of it
  (use the config map on the server, which a document that leaves out `configmap` does not touch).
