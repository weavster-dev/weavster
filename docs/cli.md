# Command-line client

`weavster` without `server`, `test`, or `config` is the command-line client. It talks to a
running server over the REST API, either as an interactive shell or running a script file (batch
mode). Checking config-as-code files needs no server: `weavster config validate FILE...` (see
[Check a document](config-as-code.md#check-a-document)).

```bash
# Interactive shell
weavster -a http://127.0.0.1:8080 -u admin -p 'A-Strong-Passw0rd'
weavster> flow list
adt	started	ADT normalize
weavster> quit

# Batch mode
weavster -a http://127.0.0.1:8080 -u admin -p 'A-Strong-Passw0rd' -s script.txt
```

| Flag | Meaning |
|---|---|
| `-a address` | Server address (default `http://127.0.0.1:8080`). Include the scheme, and the server's [context path](server-config.md#listen) if it has one: `https://weavster.internal:8443/weavster`. With `-u`/`-p` and a plain `http://` address on another machine, the client warns that the password is sent unencrypted. |
| `-u user`, `-p password` | Log in as this user. The credentials are checked at startup. If they are wrong, or the account must change its password first, the client prints `Could not log in to server.` and the server's reason, then continues (commands then fail with `401` or `403`). A password without a user exits `2`. |
| `-c file` | Connection file with the address and credentials (see below). `-a`, `-u`, `-p`, and `-ca` override its values. A missing or invalid file exits `2`. |
| `-ca file` | For an `https` address: a PEM file of CA certificates to trust besides the system's, for a server whose certificate comes from a private CA or is self-signed. A missing file, or one without a certificate, exits `2`. |
| `-s file` | Script file: one command per line. Empty lines and lines starting with `#` are skipped. |
| `-v` | Print the server's version and exit. It needs `-u`/`-p` (or `-c`), because the server only answers signed-in users; it exits `2` if the server cannot be reached or refuses the credentials. |
| `-h` | Print usage and exit. |
| `-d` | Debug mode: error messages also list each underlying cause. |

Connection file (`-c`), YAML with only these keys:

```yaml
address: https://weavster.internal:8443/weavster
user: admin
password: A-Strong-Passw0rd
ca: /etc/weavster/ca.pem          # optional: a private CA for https (a relative path is next to this file)
```

Keep it readable only by you (`chmod 600`), because it holds a password.

### Connect over HTTPS

The client speaks TLS 1.2 or later and checks the server's certificate. If the certificate comes
from a private CA (or is self-signed), pass that CA:

```bash
weavster -a https://weavster.internal:8443 -ca /etc/weavster/ca.pem -u admin -p 'A-Strong-Passw0rd'
```

Without it, a command fails with the reason and a hint:

```text
Error: Get "https://weavster.internal:8443/api/v1/system": tls: failed to verify certificate: x509: certificate signed by unknown authority
  The server's certificate is not signed by a CA this machine trusts: pass that CA with -ca FILE (or ca: in the connection file).
```

Other certificate problems get their own hint: a certificate issued for another name (connect
with a name it lists), an expired one (renew it, or check the clock), or one refused for another
reason, such as a missing server-auth key usage (reissue it). `-ca` applies to `https` addresses
only; with an `http` address the client warns that it is not used.

## Interactive shell

The shell prints the prompt `weavster> `, runs each command you type, and shows errors without
leaving. `quit`, `exit`, or end of input (Ctrl-D) ends it with exit code `0`. A line longer than
1 MiB, or a read error, ends it with exit code `2`. It accepts the same
commands as batch mode.

## Batch mode

Commands are split on spaces. Put an argument that contains spaces in double quotes, and write
`\"` and `\\` for a quote or backslash inside them: `flow rename adt "ADT \"Inbound\""`. A
missing closing quote is an error. File names are relative to the directory you run `weavster`
from.

Every command in the script runs, even after an error. The client exits `0` when every command
succeeded and `2` when any command failed: an unknown command, a usage error, a file that cannot
be read, a server that cannot be reached, or an error reply from the server. Server errors are
printed as:

```text
Error: server returned 404 Not Found: flow not found
```

Each command needs the same [permission](authentication.md#permissions) as the API call it makes.

## Flow commands

```text
# script.txt
flow create adt.json
flow deploy adt
flow start adt
flow list
```

| Command | What it does | API call |
|---|---|---|
| `flow list` | One line per flow: id, status, and name, separated by tabs. Tabs, newlines, and other control characters in a name are printed as `\t`, `\n`, … | `GET /api/v1/flows` |
| `flow get <id>` | Prints the flow as JSON. | `GET /api/v1/flows/{id}` |
| `flow create <file>` | Creates a flow from a JSON file (a [flow definition](processing-messages.md#1-create-a-flow)). | `POST /api/v1/flows` |
| `flow update <id> <file>` | Replaces a flow's definition. The flow keeps its status (and `enabled` unless the file sets it). | `PUT /api/v1/flows/{id}` |
| `flow update-all <file>` | Replaces several definitions from a `{"flows":[…]}` file; nothing is written if one is invalid or unknown. | `PUT /api/v1/flows` |
| `flow rename <id> <name>` | Changes the flow's `name` to the remaining words, joined by single spaces. It reads the definition and writes it back with the new name, so a change another user makes in between is overwritten (status and `enabled` are kept). | `GET` then `PUT /api/v1/flows/{id}` |
| `flow enable <id>`, `flow disable <id>` | Sets automatic deployment at startup. | `POST /api/v1/flows/{id}/enable`, `…/disable` |
| `flow remove <id>` | Deletes the flow and prints `removed <id>`. | `DELETE /api/v1/flows/{id}` |
| `flow export <file> [<id>…]` | Writes an export document (all flows, or the listed ids and their dependencies) to `<file>`. | `GET /api/v1/flows/export` |
| `flow import <file> [--overwrite]` | Imports an export document; `--overwrite` replaces existing flows. | `POST /api/v1/flows/import` |
| `flow deploy <id>`, and likewise `undeploy`, `start`, `stop`, `pause`, `halt`, `resume` | Changes the flow's [lifecycle](flow-lifecycle.md) status. | `POST /api/v1/flows/{id}/{action}` |
| `flow deploy-all`, and likewise `undeploy-all`, `start-all`, `stop-all`, `pause-all`, `halt-all`, `resume-all` | Runs the action on every flow it applies to and prints `changed` and `skipped` (see [Flow lifecycle](flow-lifecycle.md#act-on-all-flows-at-once)). | `POST /api/v1/flows/{action}-all` |
| `flow redeploy-all` | Undeploys and redeploys every flow that is not `undeployed`; each ends `deployed`. | `POST /api/v1/flows/redeploy-all` |
| `flow stop-destination <id> <destination>`, `flow start-destination <id> <destination>` | Holds or releases one destination's deliveries. | `POST /api/v1/flows/{id}/destinations/{name}/{stop,start}` |
| `flow connectors` | Every flow's source type (its `source.type` when it has a source, otherwise `sourceType`) and destination names. | `GET /api/v1/flows/connector-names` |
| `flow ports` | The ports the server listens on: the API, and the http sources of started flows (`flow:<id>`). | `GET /api/v1/flows/ports-in-use` |
| `flow reset-stats <id> [lifetime]` | Clears the flow's current statistics; `lifetime` also clears its lifetime totals. | `POST /api/v1/flows/{id}/stats/reset` |
| `flow stats [<id>]` | Statistics of one flow, or of every flow: `id  received=… filtered=… transformed=… sent=… errored=… queued=…`. | `GET /api/v1/flows/{id}/stats` |
| `flow help` | Lists the flow commands. | — |

Wherever a command takes `<id>`, you can also give the flow's name (spec: `id|name`). The client
first uses the argument as an id. Only if the server reports that flow missing does it look the
argument up by name (this needs the `flows:view` permission) and run the command again. An unknown
name, or a name several flows share, is an error that lists their ids.

Commands that change a flow print the server's reply (the flow, or the import/update result).
`flow export` and `flow import` use the [export document](flow-export-import.md) format.

## Deploy, import, and export (spec forms)

| Command | What it does |
|---|---|
| `exportmessages "path" <flow>` | Writes an archive of the flow's messages (by id or name; `*` for every flow), the newest 10,000, to `path`. |
| `importmessages "path" <flow>` | Imports an archive file into the flow (existing message ids are skipped). |
| `exportmap "path"`, `importmap "path"` | Writes the config map to a JSON file, or replaces it with one. See [Config map, scripts, and settings](config-items.md). |
| `exportscripts "path"`, `importscripts "path"` | The same for the global scripts. |
| `snippet list`, `snippet import "path"`, `snippet export "path"`, `snippet remove <name>` | Manages code snippets; `snippet library …` does the same for libraries. See [Code snippets and libraries](snippets.md). |
| `config validate "path"` | Checks a config-as-code document (YAML or JSON) on this machine; it needs no server or login. See [Config-as-code documents](config-as-code.md). |
| `config diff "path"`, `config plan "path"` | Shows what applying the document would change: `diff` as text (`+`, `~` with changed values, `-`), `plan` as JSON. Nothing changes. |
| `config apply "path" [--dry-run] [reason…]` | Plans the document, prints the plan, and applies it; refused if the server changed meanwhile, undone completely if a change fails. See [Apply](config-as-code.md#apply). |
| `exportcfg "path" [overwriteconfigmap]`, `importcfg "path" [nodeploy] [overwriteconfigmap] [force]` | Exports or imports the whole configuration (flows, alerts, snippets, scripts, settings, and optionally the config map). See [Export and import the whole configuration](config-transfer.md). |
| `importalert "path" [force]`, `exportalert <id, "name", or *> "path"` | Imports alerts from a JSON file (`force` replaces existing ids), or exports one alert or all of them. See [Alerts](alerts.md). |
| `deadletter list [flow]`, `deadletter show <id>` | Lists dead-lettered messages (each destination's attempts and last error), or shows one as JSON. |
| `deadletter requeue <id>`, `deadletter requeue all [flow]` | Gives dead-lettered messages another round of delivery attempts; delivered destinations are not sent again. See [Dead-lettered messages](processing-messages.md#dead-lettered-messages). |
| `deadletter remove <id>` | Deletes a dead-lettered message (refuses any other status). |
| `clearallmessages` | Removes every message. Started flows are stopped first and started again afterwards; the output names them, and counts messages kept because they were being processed. |
| `dump stats "path"`, `dump events "path"` | Writes every flow's statistics, or the newest 10,000 events, to a JSON file. |
| `resetstats [lifetime]` | Clears every flow's current statistics; `lifetime` also clears the lifetime totals. |
| `deploy [timeout]` | Deploys every flow that is `enabled` and `undeployed` (dependencies first, as `flow deploy` does); disabled flows are skipped, as at server start. Prints `deployed <id>` for each and `deployed N flows`. After `timeout` seconds no further flow is started (a deploy already sent finishes) and the command exits `2`. A flow that fails is reported, the others still deploy, and the command exits `2`. |
| `import "path" [force]` | Same as `flow import`; `force` replaces existing flows. |
| `export <id> "path"`, `export "name" "path"`, `export * "path"` | Same as `flow export`: one flow (by id or name) and its dependencies, or `*` for all flows. |

```text
deploy 60
export * "backups/all flows.json"
import "backups/all flows.json" force
```

## Other commands

| Command | What it does |
|---|---|
| `status` | Lists deployed flows (every status except `undeployed`): id, status, and name per line, or `no deployed flows`. |
| `version` | Prints the client version. |
| `user list` | One line per account: username, permissions, and `must change password` / `locked` when they apply (permission `users:admin`). |
| `user add <name> <password> [permission…]` | Creates an account; the user must change the password at the first login. |
| `user remove <name>` | Deletes an account. |
| `user changepw <name> <password>` | Sets an account's password; the user must change it at the next login. |
| `help` | Lists the commands. |
| `quit`, `exit` | End the interactive shell; ignored in batch mode. |

## Test your flows (`weavster test`)

`weavster test` runs your flows' transforms against sample messages, on your machine: no server
and no database. Keep sample messages and what each flow must make of them in **fixture files**
next to your [config-as-code documents](config-as-code.md), and run the command in CI before
`config apply`.

```bash
weavster test --format junit --output artifacts/ .
```

It looks under each `PATH` (default: the current directory, hidden directories skipped) for:

- **config-as-code documents**: `*.yaml`, `*.yml`, or `*.json` files with `version: "1"`. Their
  flows are the ones the fixtures test. Other YAML and JSON files are left alone.
- **fixture files**: `*.test.yaml`, `*.test.yml`, or `*.test.json`.

A fixture file names one flow and lists cases:

```yaml
# tests/adt.test.yaml
flow: adt
cases:
  - name: admit
    inputFile: a01.hl7              # relative to this file; or `input:` with the message inline
    expect:
      output: {patient: {lastName: DOE, mrn: "12345"}}
      excluded: []
      destinations:
        his: {output: {id: "12345"}}
  - name: update is filtered
    input: "MSH|^~\\&|LAB|H|EHR|H|20240101120000||ADT^A08|2|P|2.5\rPID|1||12345^^^MRN||DOE^JOHN\r"
    expect: {status: filtered}
  - name: not hl7
    input: hello
    expect: {status: errored, error: HL7 v2}
```

Each case runs the message through the flow exactly as the server processes it: read with the
flow's `inputFormat`, then its `transform` (filters, maps, sets, `destinationSet`, a build step),
then the own `transform` of each destination the message reaches. Nothing is stored or delivered.

| `expect` key | Checks | Default |
|---|---|---|
| `status` | `transformed`, `filtered` (a filter step dropped it), or `errored` (the input could not be read, or a step failed) | `transformed` |
| `error` | Text the error must contain (with `status: errored`) | not checked |
| `output` | The flow's JSON output has these fields with these values; fields you leave out are ignored, arrays and values must be equal | not checked |
| `outputText` | The flow's output, exactly (for a build step's HL7 v2, XML, or text) | not checked |
| `excluded` | The destinations `destinationSet` steps left out (`[]`: none) | not checked |
| `destinations.NAME` | That destination's own transform: `status`, `error`, `output`, `outputText` as above | not checked |

Each case is named `<fixture file without .test.yaml>/<case name>`, for example
`tests/adt/admit`. The built-in codec checks also run, named `identity/…`: each parses a sample
of a data format (HL7 v2, JSON, XML, delimited, raw) and must write it back byte for byte.

```text
$ weavster test --format json tests
FAIL tests/adt/admit: output differs at patient.lastName: it is {"patient":{"lastName":"DOE",…}}, want the fields {"patient":{"lastName":"SMITH"}}
[
  {"name": "identity/hl7", "passed": true},
  …
  {"name": "tests/adt/admit", "passed": false, "failure": "output differs at patient.lastName: …"}
]
```

- `--format junit` (default) writes JUnit XML, `--format json` JSON; `--output DIR` writes
  `results.xml` or `results.json` into `DIR` instead of printing it. Failures are also printed
  to stderr, one line each (`FAIL name: reason`).
- `--filter TEXT` runs the cases whose name contains `TEXT`. If none does, the command fails
  with `no case matches --filter "TEXT"`.
- A fixture that cannot be run fails with the reason: a flow no document defines, a flow defined
  in two documents, an unknown key (`field casez not found`), a case without a name, or a missing
  `inputFile`. A config-as-code document that does not parse fails as well.

### Pitfalls

- A fixture only sees the documents under the paths you give. Pass the directory that holds both
  (`weavster test .`), not just the tests directory.
- `output` compares JSON values: `"12345"` (text) and `12345` (a number) differ. HL7 v2 fields
  are text.
- A flow without a `transform` passes messages through unchanged, so any input is `transformed`.

## Deprecated command names

Scripts written for older tools can keep their command names. A deprecated name prints a warning
on stderr and then runs its replacement with the same arguments; the warning does not change the
exit code.

| Deprecated | Runs |
|---|---|
| `channel …` | `flow …` |
| `codetemplate …` | `snippet …` |

```text
weavster> channel list
Warning: "channel" is deprecated; use "flow"
adt	started	ADT Inbound
```

Update your scripts to the new names; the old ones may be removed in a later release.

## Exit codes and errors

| Command | `0` | `1` | `2` |
|---|---|---|---|
| `weavster -s script` (batch) | Every command succeeded | — | Any command failed (the script still runs to the end), a line longer than 1 MiB (the script stops there), an unknown flag, or a missing connection file |
| `weavster` (interactive shell) | `quit`, `exit`, or end of input, even after failed commands (their errors are shown) | — | A line longer than 1 MiB, a read error, an unknown flag, or a missing connection file |
| `weavster server` | `-h`, or a clean stop on SIGINT/SIGTERM | The configuration is invalid, the server could not start (store, TLS, bootstrap), or it runs as a privileged user without `WEAVSTER_ALLOW_ROOT=1` | An unknown flag or extra arguments |
| `weavster test` | Every case passed, or `-h` | A case or fixture failed, a config-as-code document did not parse, or `--filter` matched nothing | An unknown flag, `--format` other than `junit` or `json`, a `PATH` that does not exist, or the results could not be written |
| `weavster config validate FILE...` | Every file is valid, or `-h` | A file is invalid | No file given, a file cannot be read or is larger than 50 MiB, or another `config` command (`diff`, `plan`, and `apply` need a server: run them in the shell or with `-s`) |

`-h` (or `--help`) prints usage and exits `0` for every command. Usage errors are checked
before anything else runs.

Errors go to stderr and start with `Error:`; an unknown flag is followed by the list of flags
(`Error: flag provided but not defined: -nope`). A reply from the server shows its status and
message, for example `Error: server returned 404 Not Found: snippet not found`. With `-d`, the
client also prints each underlying cause:

```text
Error: Get "http://127.0.0.1:8080/api/v1/flows": dial tcp 127.0.0.1:8080: connect: connection refused
  caused by *net.OpError: dial tcp 127.0.0.1:8080: connect: connection refused
  caused by *os.SyscallError: connect: connection refused
  caused by syscall.Errno: connection refused
```

A failed login prints `Could not log in to server.` and continues (the prompt or the script);
the commands that follow then fail with the server's `401` reply.
