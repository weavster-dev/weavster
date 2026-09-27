# CI/CD

Keep the server's configuration in a repository as a [config-as-code document](config-as-code.md)
(`weavster.yaml`) and let CI do the rest:

- **On a pull request or merge request** the pipeline shows what applying the changed document
  would do to the server ([`config diff`](cli.md)). Nothing changes on the server. An invalid
  document fails the job with the reason.
- **On merge to `main`** the pipeline applies the document ([`config apply`](config-as-code.md#apply)).
  The apply plans again first, so it applies what the server needs at that moment, and records the
  commit in the [audit log](audit-log.md) as the reason. If any change fails, the whole apply is
  undone.

Copy a sample below into the repository that holds `weavster.yaml`; the files are also available
to download: [`github-actions.yml`](examples/ci/github-actions.yml),
[`gitlab-ci.yml`](examples/ci/gitlab-ci.yml), [`weavster.yaml`](examples/ci/weavster.yaml).

## Before you start

1. A server reachable from CI over **HTTPS** (`listen.tlsAddress`, see
   [Server configuration](server-config.md)). Plain HTTP would send the password in cleartext.
2. A user for CI. The plan job needs the [config plan permissions](config-as-code.md#see-what-would-change)
   (`flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit`,
   `configmap:edit`); the apply job also needs `flows:edit`. Give CI its own user, so its applies
   are easy to find in the audit log.
3. Three secrets in the CI system: `WEAVSTER_ADDRESS` (for example
   `https://weavster.example.com`), `WEAVSTER_USER`, `WEAVSTER_PASSWORD`. The jobs write them to
   a [connection file](cli.md) readable only by the job, so the password never appears on a
   command line or in the log.

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

## GitHub Actions

Save as `.github/workflows/weavster.yml`. The plan appears in the job summary of the pull
request's checks. Add required reviewers to the `production` environment to have a person approve
each apply.

```yaml
# Weavster config-as-code on GitHub Actions: plan on pull requests, apply on
# merge to main. Save as .github/workflows/weavster.yml in the repository that
# holds weavster.yaml.
#
# Repository secrets: WEAVSTER_ADDRESS (https://weavster.example.com),
# WEAVSTER_USER, WEAVSTER_PASSWORD. The user needs the config plan
# permissions, plus the config apply permissions for the apply job.
name: weavster-config

on:
  pull_request:
    paths: [weavster.yaml]
  push:
    branches: [main]
    paths: [weavster.yaml]

permissions:
  contents: read

# One apply at a time; a queued apply waits instead of being cancelled.
concurrency:
  group: weavster-config-${{ github.event_name }}
  cancel-in-progress: false

env:
  WEAVSTER_VERSION: latest # pin a release tag in production

jobs:
  plan:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Install weavster
        run: go install "github.com/weavster-dev/weavster/cmd/weavster@${WEAVSTER_VERSION}"
      - name: Write the connection file
        env:
          WEAVSTER_ADDRESS: ${{ secrets.WEAVSTER_ADDRESS }}
          WEAVSTER_USER: ${{ secrets.WEAVSTER_USER }}
          WEAVSTER_PASSWORD: ${{ secrets.WEAVSTER_PASSWORD }}
        run: |
          umask 077
          quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"; }
          {
            echo "address: $(quote "$WEAVSTER_ADDRESS")"
            echo "user: $(quote "$WEAVSTER_USER")"
            echo "password: $(quote "$WEAVSTER_PASSWORD")"
          } > "$RUNNER_TEMP/weavster.conn"
      - name: Plan
        run: |
          printf 'config diff "weavster.yaml"\n' > "$RUNNER_TEMP/plan.txt"
          status=0
          weavster -c "$RUNNER_TEMP/weavster.conn" -s "$RUNNER_TEMP/plan.txt" > plan.out || status=$?
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
    environment: production # add required reviewers here to gate applies
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Install weavster
        run: go install "github.com/weavster-dev/weavster/cmd/weavster@${WEAVSTER_VERSION}"
      - name: Write the connection file
        env:
          WEAVSTER_ADDRESS: ${{ secrets.WEAVSTER_ADDRESS }}
          WEAVSTER_USER: ${{ secrets.WEAVSTER_USER }}
          WEAVSTER_PASSWORD: ${{ secrets.WEAVSTER_PASSWORD }}
        run: |
          umask 077
          quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"; }
          {
            echo "address: $(quote "$WEAVSTER_ADDRESS")"
            echo "user: $(quote "$WEAVSTER_USER")"
            echo "password: $(quote "$WEAVSTER_PASSWORD")"
          } > "$RUNNER_TEMP/weavster.conn"
      - name: Apply
        run: |
          printf 'config apply "weavster.yaml" %s\n' "merged ${GITHUB_SHA}" > "$RUNNER_TEMP/apply.txt"
          weavster -c "$RUNNER_TEMP/weavster.conn" -s "$RUNNER_TEMP/apply.txt"
```

## GitLab CI

Save as `.gitlab-ci.yml`. Mark `WEAVSTER_PASSWORD` as masked and protected, and protect `main`,
so only merges can apply. The plan is in the merge request pipeline's job log.

```yaml
# Weavster config-as-code on GitLab CI: plan on merge requests, apply on
# merge to main. Save as .gitlab-ci.yml in the repository that holds
# weavster.yaml.
#
# CI/CD variables (masked; WEAVSTER_PASSWORD also protected):
# WEAVSTER_ADDRESS (https://weavster.example.com), WEAVSTER_USER,
# WEAVSTER_PASSWORD. The user needs the config plan permissions, plus the
# config apply permissions for the apply job.
stages: [plan, apply]

variables:
  WEAVSTER_VERSION: latest # pin a release tag in production

.weavster:
  image: golang:1.22
  before_script:
    - go install "github.com/weavster-dev/weavster/cmd/weavster@${WEAVSTER_VERSION}"
    - |
      umask 077
      quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"; }
      {
        echo "address: $(quote "$WEAVSTER_ADDRESS")"
        echo "user: $(quote "$WEAVSTER_USER")"
        echo "password: $(quote "$WEAVSTER_PASSWORD")"
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
  resource_group: weavster-config # one apply at a time
  environment: production
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
      changes: [weavster.yaml]
  script:
    - printf 'config apply "weavster.yaml" %s\n' "merged ${CI_COMMIT_SHORT_SHA}" > /tmp/apply.txt
    - weavster -c /tmp/weavster.conn -s /tmp/apply.txt
```

## Pitfalls

- **Pin the version.** `WEAVSTER_VERSION: latest` installs the newest client. Pin the release tag
  that matches your server so a new client cannot change behaviour under you.
- **The server moved on.** If someone changes the server between the plan and the merge, the
  apply job plans again and applies the current differences, which may be more than the pull
  request showed. Check the apply job's output; to be warned instead, run the pull request plan
  again just before merging.
- **Sections you leave out are not managed.** Deleting a whole section from `weavster.yaml` does
  not remove those artifacts from the server; keep the section with no entries (`scripts: {}`)
  to remove them all.
- **One apply at a time.** The samples queue applies (`concurrency`, `resource_group`); do not
  run applies from two pipelines against one server in parallel.
- **Secrets in the document.** `weavster.yaml` is committed: keep passwords and tokens out of it
  (use the config map on the server, which a document that leaves out `configmap` does not touch).
