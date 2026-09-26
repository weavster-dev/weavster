# Command-line client

`weavster` without `server` or `test` is the command-line client. It talks to a running server
over the REST API, either as an interactive shell or running a script file (batch mode).

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

## Interactive shell

The shell prints the prompt `weavster> `, runs each command you type, and shows errors without
leaving. `quit`, `exit`, or end of input (Ctrl-D) ends it with exit code `0`. A line longer than
1 MiB, or a read error, ends it with exit code `2`. It accepts the same
commands as batch mode.

## Batch mode

Commands are split on spaces: arguments (such as file names) cannot contain spaces, and there is
no quoting. File names are relative to the directory you run `weavster` from.

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
| `flow deploy\|undeploy\|start\|stop\|pause\|halt\|resume <id>` | Changes the flow's [lifecycle](flow-lifecycle.md) status. | `POST /api/v1/flows/{id}/{action}` |
| `flow redeploy-all` | Undeploys and redeploys every flow that is not `undeployed`; each ends `deployed`. | `POST /api/v1/flows/redeploy-all` |
| `flow stop-destination <id> <destination>`, `flow start-destination <id> <destination>` | Holds or releases one destination's deliveries. | `POST /api/v1/flows/{id}/destinations/{name}/{stop,start}` |
| `flow connectors` | Every flow's source type and destination names. | `GET /api/v1/flows/connector-names` |
| `flow ports` | The ports the server listens on. | `GET /api/v1/flows/ports-in-use` |
| `flow help` | Lists the flow commands. | — |

`export`, `import`, `redeploy-all`, `connector-names`, and `ports-in-use` are not flow ids; using
one as `<id>` is an error.

Commands that change a flow print the server's reply (the flow, or the import/update result).
`flow export` and `flow import` use the [export document](flow-export-import.md) format.

## Other commands

| Command | What it does |
|---|---|
| `status` | Lists deployed flows (every status except `undeployed`): id, status, and name per line, or `no deployed flows`. |
| `version` | Prints the client version. |
| `user list` | Prints nothing yet. |
| `help` | Lists the commands. |
| `quit`, `exit` | End the interactive shell; ignored in batch mode. |

## Limits today

- `user list` prints nothing yet.
