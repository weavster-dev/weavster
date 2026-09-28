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
| `-a address` | Server address (default `http://127.0.0.1:8080`). Include the scheme. |
| `-u user`, `-p password` | Log in as this user. The credentials are checked at startup. If they are wrong, or the account must change its password first, the client prints `Could not log in to server.` and the server's reason, then continues (commands then fail with `401` or `403`). A password without a user exits `2`. |
| `-c file` | Connection file with the address and credentials (see below). `-a`, `-u`, and `-p` override its values. A missing or invalid file exits `2`. |
| `-s file` | Script file: one command per line. Empty lines and lines starting with `#` are skipped. |
| `-v` | Print the server's version and exit. It needs `-u`/`-p` (or `-c`), because the server only answers signed-in users; it exits `2` if the server cannot be reached or refuses the credentials. |
| `-h` | Print usage and exit. |
| `-d` | Debug mode: error messages also list each underlying cause. |

Connection file (`-c`), YAML with only these keys:

```yaml
address: http://127.0.0.1:8080
user: admin
password: A-Strong-Passw0rd
```

Keep it readable only by you (`chmod 600`), because it holds a password.

Use the final API address in `-a` or the connection file, for example
`https://weavster.example.com`. The client refuses HTTP redirects and reports the
original `3xx` status as an error, so credentials and request bodies cannot be
forwarded by a redirect. If your proxy redirects to a different address, configure
that final address directly.

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
| `weavster test` | Every fixture passed, or `-h` | A fixture failed | An unknown flag, or the results could not be written |
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
