# Config-as-code documents

A config-as-code document describes a server's configuration in one YAML (or JSON) file that
you keep in version control: flows, alerts, code snippets and libraries, global scripts, the
config map, and settings. Every artifact has the same shape it has in the API.

!!! note "Checked, not applied yet"
    Today you can check a document with `config validate`. Applying it to a server is not
    available yet; to copy configuration between servers use
    [export and import](config-transfer.md).

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

## Editor support

JSON Schemas for the document and each artifact kind are published in the repository under
`agent-docs/schemas/`: `config.schema.json`, `flow.schema.json`, `alert.schema.json`,
`snippet.schema.json`, and `snippet-library.schema.json`. Point your editor's YAML/JSON schema
support at `config.schema.json` for completion and inline errors. The schemas check shapes;
`config validate` also checks the rules that span artifacts (for example, that a snippet's
library exists).
