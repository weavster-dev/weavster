# Config-as-code documents

A config-as-code document describes a server's configuration in one YAML (or JSON) file that
you keep in version control: flows, alerts, code snippets and libraries, global scripts, the
config map, and settings. Every artifact has the same shape it has in the API.

You check a document (`config validate`), see what applying it would change (`config diff`,
`config plan`), and apply it (`config apply`). To copy a whole configuration between servers
without a document, use [export and import](config-transfer.md).

## Example

```yaml
version: "1"
flows:
  adt:                       # the key is the flow id
    name: ADT Inbound
    enabled: true
    destinations:
      - {name: ehr, type: http, url: https://ehr.example.com/in}
alerts:
  adt-errors:                # the key is the alert id
    name: ADT errors
    enabled: true
    trigger: {events: [message.errored], flows: [adt]}
    actions: [{type: email, to: [ops@example.com]}]
snippetLibraries:
  hl7: {description: HL7 helpers}
snippets:
  pid: {library: hl7, code: "get('PID.3')"}
scripts:
  deploy: log('deployed')
configmap:
  region: eu
settings:
  retention: {days: 30}
```

## Sections

| Section | Keyed by | Each value |
|---|---|---|
| `flows` | flow id | A flow, as in the [flow API](processing-messages.md#1-create-a-flow) (no `status`). |
| `alerts` | alert id | An [alert](alerts.md). |
| `snippetLibraries` | library name | A [snippet library](snippets.md). |
| `snippets` | snippet name | A [snippet](snippets.md); its `library` must be in `snippetLibraries`. |
| `scripts` | script name | The script source (text). |
| `configmap` | entry name | A text value. |
| `settings` | setting name | Any JSON value except `null`. |

- Every section is optional; `version` defaults to `"1"`, the only version.
- The file holds one YAML document: a second one after `---` is an error, and so is an empty
  file.
- An artifact's `id` (flows, alerts) or `name` (snippets, libraries) defaults to its key. If you
  write it, it must equal the key.
- Names are 1–128 characters from `A-Z a-z 0-9 . _ -`.
- Unknown fields are errors anywhere in the document, so a typo such as `recipient:` is caught
  instead of ignored.

## Check a document

From the command-line client:

```text
weavster> config validate "weavster.yaml"
weavster.yaml is valid: 1 flows, 1 alerts, 1 snippets, 1 snippet libraries, 1 scripts, 1 config map entries, 1 settings
```

Or with the API (permission `flows:edit`; nothing on the server changes):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' --data-binary @weavster.yaml \
  http://127.0.0.1:8080/api/v1/config/validate
```

```json
{"counts":{"flows":1,"alerts":1,"snippets":1,"snippetLibraries":1,"scripts":1,"configmap":1,"settings":1},"valid":true}
```

An invalid document returns `400`, and the message names each problem with its place in the
document, for example:

```text
Error: server returned 400 Bad Request: config: alerts.adt-errors: alert adt-errors: unknown trigger event "message.sent"; use [message.errored message.queued message.dead-lettered]
```

## See what would change

`config diff` compares the document with the server's live configuration and shows what
applying it would change. Nothing on the server changes.

```text
weavster> config diff "weavster.yaml"
~ configmap/region
    value: "us" → "eu"
~ flow/adt
    destinations[0].url: "https://old.example.com/in" → "https://ehr.example.com/in"
    name: "ADT" → "ADT Inbound"
+ flow/lab
- flow/legacy
1 to add, 2 to change, 1 to remove, 4 unchanged
```

- `+` adds, `~` changes (one line per changed value), `-` removes. Adds and changes come first,
  sorted by key, then removals; changed values are sorted by path.
- **A section you leave out is not managed.** Nothing in it is changed or removed. A section you
  include is managed completely: an artifact the server has but the section lacks is removed.
  Write `settings: {}` (or `settings:` with nothing under it) to remove every setting; leave
  `settings` out to keep them.
- Flows are compared without their runtime state, so a deployed flow is not "changed" because
  it is deployed. Formatting and key order never count as changes.

`config plan` prints the same plan as JSON for scripts and CI:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' --data-binary @weavster.yaml \
  http://127.0.0.1:8080/api/v1/config/plan
```

```json
{"fingerprint":"78ff2b…","added":["flow/lab"],"updated":["configmap/region","flow/adt"],
 "removed":["flow/legacy"],"unchanged":4,
 "changes":[{"key":"configmap/region","action":"update","before":"us","after":"eu",
             "fields":[{"path":"value","before":"us","after":"eu"}]}, …],
 "text":"~ configmap/region\n    value: \"us\" → \"eu\"\n…"}
```

Artifact keys are `flow/…`, `alert/…`, `snippet/…`, `library/…`, `script/…`, `configmap/…`,
and `settings/…`. `fingerprint` identifies the live configuration the plan was made against: it
changes whenever anything in it changes. Planning needs the permissions an
[export](config-transfer.md#permissions) with the config map needs: `flows:view`,
`alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit`, and `configmap:edit`.

To plan against the server's [Git repository](git.md#plan-and-apply-from-the-repository)
instead of a document you send, pass `gitRev` (a revision; empty means `HEAD`) with no body.

## Apply

```text
weavster> config apply "weavster.yaml" CHG-1234 rotate EHR endpoint
~ flow/adt
    destinations[0].url: "https://old.example.com/in" → "https://ehr.example.com/in"
0 to add, 1 to change, 0 to remove, 6 unchanged
applied 1 changes
```

`config apply` plans the document, prints the plan, and applies exactly that plan. Words after
the path (other than `--dry-run`) are the reason, recorded in the audit log; a word starting
with `-` that is not `--dry-run` is refused, so a mistyped flag never applies.

- **Stale plans are refused.** The plan's `fingerprint` covers both the live configuration and
  the document, and goes with the apply. If either changed in between, the apply is refused with
  `409` and nothing changes; plan again and review the new plan.
- **All or nothing.** Changes are applied in a safe order: libraries, snippets, flows (all added
  and changed flows together, so new flows may depend on each other), alerts, then scripts,
  config map, and settings; removals come last, and a flow is removed before the flows it uses.
  If any change fails, every change already made is undone and the reply says which change
  failed (`409` for a problem in the configuration, such as removing a flow that a flow you keep
  depends on; `500` for a server problem). If undoing a change fails, the others are still
  undone, and the reply names what could not be undone: the configuration is partly applied, so
  plan again to see where it stands.
- One apply runs at a time, but other API calls are not blocked: avoid editing the same flows,
  alerts, or items through the API while an apply runs, because a rollback puts back the values
  from the plan.
- New flows are created undeployed; deploy them as usual. A removed flow that was running is
  undeployed first; if the apply is rolled back, the flow comes back undeployed and the reply
  lists it so you can deploy it again.
- `--dry-run` checks that the plan is still current and changes nothing.

With the API, send the plan's fingerprint:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' --data-binary @weavster.yaml \
  'http://127.0.0.1:8080/api/v1/config/apply?fingerprint=78ff2b…&reason=CHG-1234'
```

```json
{"applied":true,"plan":{"fingerprint":"78ff2b…","added":[],"updated":["flow/adt"], …}}
```

Applying needs `flows:view`, `flows:edit`, `alerts:edit`, `snippets:edit`, `scripts:edit`,
`settings:edit`, and `configmap:edit`. Every attempt is written to the
[audit log](audit-log.md) as `POST /api/v1/config/apply`, with the planned keys
(`plan.added`, `plan.updated`, `plan.removed`), the fingerprint, the reason, and the result:
`applied`, `dry run`, `stale`, `invalid`, `refused` (a bad parameter), `failed` (the live
configuration could not be read), `rolled back`, or `rollback failed`.

## Editor support

JSON Schemas for the document and each artifact kind are published in the repository under
`agent-docs/schemas/`: `config.schema.json`, `flow.schema.json`, `alert.schema.json`,
`snippet.schema.json`, and `snippet-library.schema.json`. Point your editor's YAML/JSON schema
support at `config.schema.json` for completion and inline errors. The schemas check shapes;
`config validate` also checks the rules that span artifacts (for example, that a snippet's
library exists).
